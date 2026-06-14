package perfscan

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/glade-sh/glade/internal/apexast"
	"github.com/glade-sh/glade/internal/diagnostic"
	"github.com/glade-sh/glade/internal/typesys"
)

var (
	sourceVarDeclRe          = regexp.MustCompile(`(?i)\b([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?(?:<[^;\n=]+>)?)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:=|;|,|:)`)
	sourceDMLTargetRe        = regexp.MustCompile(`(?i)\b(?:insert|update|upsert|delete|undelete|merge)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	sourceSOQLRe             = regexp.MustCompile(`(?is)\[\s*SELECT\b.*?\]`)
	sourceGlobalDescribeRe   = regexp.MustCompile(`(?i)\bSchema\s*\.\s*getGlobalDescribe\s*\(`)
	sourceDescribeSObjectsRe = regexp.MustCompile(`(?i)\bSchema\s*\.\s*describeSObjects\s*\(`)
	sourceGetAllRe           = regexp.MustCompile(`(?i)\b[A-Za-z_][A-Za-z0-9_]*(?:__mdt|__c)\s*\.\s*getAll\s*\(`)
)

type sourceMethodFact struct {
	id         NodeID
	className  string
	methodName string
	file       string
	namespace  string
	startLine  int
	endLine    int
	start      int
	end        int
	paramTypes []string
	localTypes map[string]string
}

type sourceCallSignature struct {
	argCount int
	argTypes []string
}

type sourceStaticInitFact struct {
	id        NodeID
	className string
	file      string
	line      int
}

type sourceGraphBuilder struct {
	graph            *Graph
	index            typesys.Index
	typeSymbols      map[string]typesys.TypeSymbol
	methods          []*sourceMethodFact
	methodsByKey     map[string][]*sourceMethodFact
	methodsByID      map[NodeID]*sourceMethodFact
	callTargets      map[NodeID]map[NodeID]struct{}
	operationMethods map[NodeID]NodeID
	perRecordSeeds   map[NodeID]struct{}
	staticInits      map[string]sourceStaticInitFact
	staticFields     map[string]map[string]struct{}
	sourceByFile     map[string]string
}

func BuildSourceGraph(parsed apexast.Result, index typesys.Index) *Graph {
	builder := newSourceGraphBuilder(index)
	builder.collectDeclarations(parsed)
	builder.scanSources(parsed)
	builder.addStaticFirstTouchEdges()
	builder.propagatePerRecord()
	return builder.graph
}

func newSourceGraphBuilder(index typesys.Index) *sourceGraphBuilder {
	builder := &sourceGraphBuilder{
		graph:            NewGraph(),
		index:            index,
		typeSymbols:      make(map[string]typesys.TypeSymbol),
		methodsByKey:     make(map[string][]*sourceMethodFact),
		methodsByID:      make(map[NodeID]*sourceMethodFact),
		callTargets:      make(map[NodeID]map[NodeID]struct{}),
		operationMethods: make(map[NodeID]NodeID),
		perRecordSeeds:   make(map[NodeID]struct{}),
		staticInits:      make(map[string]sourceStaticInitFact),
		staticFields:     make(map[string]map[string]struct{}),
		sourceByFile:     make(map[string]string),
	}
	for _, typ := range index.Types {
		if typ.Name == "" {
			continue
		}
		builder.typeSymbols[sourceTypeKey(typ.Name)] = typ
		builder.typeSymbols[sourceTypeKey(shortTypeName(typ.Name))] = typ
	}
	return builder
}

func (b *sourceGraphBuilder) collectDeclarations(parsed apexast.Result) {
	for _, file := range parsed.Files {
		source := b.sourceForFile(file.Path)
		for _, decl := range file.Declarations {
			b.collectDeclaration(file.Path, source, decl, nil)
		}
	}
}

func (b *sourceGraphBuilder) collectDeclaration(file, source string, decl apexast.Declaration, classStack []string) {
	switch decl.Kind {
	case apexast.DeclarationTrigger:
		entry := b.graph.AddNode(Node{
			Kind:      NodeEntryPoint,
			Name:      decl.Name,
			File:      file,
			Line:      decl.Range.Start.Line,
			Operation: string(EntryTrigger),
		})
		method := b.addMethod(sourceMethodFact{
			className:  decl.Name,
			methodName: "<trigger>",
			file:       file,
			startLine:  decl.Range.Start.Line,
			endLine:    decl.Range.End.Line,
			start:      decl.Range.Start.Offset,
			end:        decl.Range.End.Offset,
			localTypes: map[string]string{},
		})
		b.graph.AddEdge(entry, method.id, EdgeExecutes)
	case apexast.DeclarationClass, apexast.DeclarationInterface, apexast.DeclarationEnum:
		className := decl.Name
		if len(classStack) > 0 {
			className = strings.Join(append(append([]string{}, classStack...), decl.Name), ".")
		}
		nextStack := append(append([]string{}, classStack...), decl.Name)
		for _, member := range decl.Members {
			switch member.Kind {
			case apexast.DeclarationMethod, apexast.DeclarationConstructor:
				methodName := member.Name
				if methodName == "" {
					methodName = decl.Name
				}
				method := b.addMethod(sourceMethodFact{
					className:  className,
					methodName: methodName,
					file:       file,
					namespace:  b.namespaceForClass(className),
					startLine:  member.Range.Start.Line,
					endLine:    member.Range.End.Line,
					start:      member.Range.Start.Offset,
					end:        member.Range.End.Offset,
					paramTypes: sourceParamTypes(member),
					localTypes: sourceLocalTypes(source, member),
				})
				if entryKind, ok := b.entryKindForMethod(className, member); ok {
					entry := b.graph.AddNode(Node{
						Kind:      NodeEntryPoint,
						Name:      className + "." + methodName,
						File:      file,
						Line:      member.Range.Start.Line,
						Namespace: method.namespace,
						Operation: string(entryKind),
					})
					b.graph.AddEdge(entry, method.id, EdgeExecutes)
				}
			case apexast.DeclarationField:
				if sourceHasModifier(member.Modifiers, "static") {
					b.addStaticField(className, member.Name)
					b.recordStaticWork(className, file, source, member.Range, "static field "+member.Name)
				}
			case apexast.DeclarationInitializer:
				if sourceHasModifier(member.Modifiers, "static") {
					b.recordStaticWork(className, file, source, member.Range, "static initializer")
				}
			case apexast.DeclarationClass, apexast.DeclarationInterface, apexast.DeclarationEnum:
				b.collectDeclaration(file, source, member, nextStack)
			}
		}
	}
}

func (b *sourceGraphBuilder) addMethod(method sourceMethodFact) *sourceMethodFact {
	method.file = filepath.ToSlash(method.file)
	method.id = b.graph.AddNode(Node{
		Kind:      NodeMethod,
		Name:      method.className + "." + method.methodName,
		File:      method.file,
		Line:      method.startLine,
		Namespace: method.namespace,
	})
	if method.localTypes == nil {
		method.localTypes = make(map[string]string)
	}
	copyMethod := method
	ptr := &copyMethod
	b.methods = append(b.methods, ptr)
	b.methodsByID[ptr.id] = ptr
	for _, key := range sourceMethodKeys(method.className, method.methodName) {
		b.methodsByKey[key] = append(b.methodsByKey[key], ptr)
	}
	return ptr
}

func (b *sourceGraphBuilder) scanSources(parsed apexast.Result) {
	parser := apexast.NewParser()
	for _, file := range parsed.Files {
		source := b.sourceForFile(file.Path)
		if source == "" {
			continue
		}
		astFile := parser.ParseSourceAST(file.Path, source)
		b.scanASTNodes(file.Path, source, astFile.Nodes, 0)
	}
}

func (b *sourceGraphBuilder) scanASTNodes(file, source string, nodes []apexast.ASTNode, loopDepth int) {
	for _, node := range nodes {
		b.scanASTNode(file, source, node, loopDepth)
	}
}

func (b *sourceGraphBuilder) scanASTNode(file, source string, node apexast.ASTNode, loopDepth int) {
	if _, ok := loopNodeKinds[node.Kind]; ok {
		b.scanASTNodes(file, source, node.Children, loopDepth+1)
		return
	}

	method := b.methodByLine(file, node.Range.Start.Line)
	switch node.Kind {
	case "query_expression":
		if method != nil {
			b.addOperation(method, NodeSOQL, nodeText(source, node), node.Range.Start.Line, loopDepth > 0)
		}
	case "dml_expression":
		if method != nil {
			b.addOperation(method, NodeDML, nodeText(source, node), node.Range.Start.Line, loopDepth > 0)
		}
	case "method_invocation":
		if method != nil {
			b.recordMethodInvocation(method, source, node, loopDepth > 0)
		}
	}

	b.scanASTNodes(file, source, node.Children, loopDepth)
}

func (b *sourceGraphBuilder) recordMethodInvocation(method *sourceMethodFact, source string, node apexast.ASTNode, inLoop bool) {
	methodName, receiver := methodInvocationParts(node, source)
	if methodName == "" {
		return
	}
	call := sourceCallSignatureFromText(method, nodeText(source, node))
	if receiver == nil {
		if target := b.resolveSameClassCall(method, methodName, call); target != nil {
			b.addCall(method, target, inLoop)
		}
		return
	}
	segments := identifierSegments(*receiver, source)
	if len(segments) == 0 {
		return
	}
	methodLower := strings.ToLower(strings.TrimSpace(methodName))
	if _, ok := platformDmlMethods[methodLower]; ok && sourceReceiverIsType(segments, "Database") {
		b.addOperation(method, NodeDML, nodeText(source, node), node.Range.Start.Line, inLoop)
		return
	}
	if _, ok := platformDescribeMethods[methodLower]; ok && sourceReceiverIsType(segments, "Schema") {
		b.addOperation(method, NodeDescribe, nodeText(source, node), node.Range.Start.Line, inLoop)
		return
	}
	if methodLower == "getdescribe" && sourceReceiverContains(segments, "SObjectType") {
		b.addOperation(method, NodeDescribe, nodeText(source, node), node.Range.Start.Line, inLoop)
		return
	}

	if target := b.resolveCall(method, segments, methodName, call); target != nil {
		b.addCall(method, target, inLoop)
		return
	}
	b.graph.AddEvidence(method.id, Evidence{Kind: "static", Message: "unresolved call edge", Value: strings.Join(segments, ".") + "." + methodName})
}

func (b *sourceGraphBuilder) resolveSameClassCall(method *sourceMethodFact, methodName string, call sourceCallSignature) *sourceMethodFact {
	candidates := b.methodsByKey[sourceMethodKey(method.className, methodName)]
	return selectSourceMethodCandidate(candidates, call)
}

func (b *sourceGraphBuilder) resolveCall(method *sourceMethodFact, receiver []string, methodName string, call sourceCallSignature) *sourceMethodFact {
	if len(receiver) == 0 {
		return nil
	}
	typeName := receiver[0]
	if localType := method.localTypes[strings.ToLower(typeName)]; localType != "" {
		typeName = localType
	}
	candidates := b.methodsByKey[sourceMethodKey(typeName, methodName)]
	if len(candidates) == 0 {
		candidates = b.methodsByKey[sourceMethodKey(shortTypeName(typeName), methodName)]
	}
	if len(candidates) == 0 {
		return nil
	}
	return selectSourceMethodCandidate(candidates, call)
}

func (b *sourceGraphBuilder) addCall(from, to *sourceMethodFact, perRecord bool) {
	if from == nil || to == nil {
		return
	}
	b.graph.AddEdge(from.id, to.id, EdgeCalls)
	if b.callTargets[from.id] == nil {
		b.callTargets[from.id] = make(map[NodeID]struct{})
	}
	b.callTargets[from.id][to.id] = struct{}{}
	if perRecord {
		b.perRecordSeeds[to.id] = struct{}{}
		b.graph.AddEvidence(to.id, Evidence{Kind: "static", Message: "called from loop", Value: from.className + "." + from.methodName})
	}
}

func (b *sourceGraphBuilder) addOperation(method *sourceMethodFact, kind NodeKind, text string, line int, directLoop bool) NodeID {
	name := sourceOperationName(kind, text)
	operation := name
	if kind == NodeDML {
		objects := sourceDMLObjectNames(method, text)
		if len(objects) == 0 {
			objects = sourceDMLObjectNames(method, b.sourceLine(method.file, line))
		}
		for _, object := range objects {
			operation += " object:" + object
		}
	}
	id := b.graph.AddNode(Node{
		Kind:      kind,
		Name:      name,
		File:      method.file,
		Line:      line,
		Namespace: method.namespace,
		Operation: operation,
	})
	b.graph.AddEdge(method.id, id, EdgeExecutes)
	b.operationMethods[id] = method.id
	switch kind {
	case NodeSOQL:
		b.graph.AddResourceRisk(id, ResourceRisk{DBTime: true, DBRows: true, SharedLimit: true})
		b.graph.AddEvidence(id, Evidence{Kind: "soql", Message: "query", Value: strings.TrimSpace(text)})
	case NodeDML:
		b.graph.AddResourceRisk(id, ResourceRisk{DBTime: true, Locks: true, SharedLimit: true})
		b.graph.AddEvidence(id, Evidence{Kind: "dml", Message: "operation", Value: strings.TrimSpace(text)})
	case NodeDescribe:
		b.graph.AddResourceRisk(id, ResourceRisk{CPU: true, Heap: true, SharedLimit: true})
		b.graph.AddEvidence(id, Evidence{Kind: "static", Message: "describe call", Value: strings.TrimSpace(text)})
	}
	if directLoop {
		b.graph.AddEvidence(id, Evidence{Kind: "static", Message: "direct loop body"})
	}
	return id
}

func (b *sourceGraphBuilder) propagatePerRecord() {
	visited := make(map[NodeID]struct{})
	queue := make([]NodeID, 0, len(b.perRecordSeeds))
	for seed := range b.perRecordSeeds {
		queue = append(queue, seed)
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if _, ok := visited[current]; ok {
			continue
		}
		visited[current] = struct{}{}
		for opID, methodID := range b.operationMethods {
			if methodID != current {
				continue
			}
			node, ok := b.graph.node(opID)
			if !ok {
				continue
			}
			switch node.Kind {
			case NodeSOQL:
				b.graph.AddEvidence(opID, Evidence{Kind: "static", Message: "query in per-record path", Value: b.methodLabel(current)})
			case NodeDML:
				b.graph.AddEvidence(opID, Evidence{Kind: "static", Message: "dml in per-record path", Value: b.methodLabel(current)})
			}
		}
		for target := range b.callTargets[current] {
			if _, ok := visited[target]; !ok {
				queue = append(queue, target)
			}
		}
	}
}

func (b *sourceGraphBuilder) addStaticField(className, fieldName string) {
	if className == "" || fieldName == "" {
		return
	}
	key := sourceTypeKey(className)
	if b.staticFields[key] == nil {
		b.staticFields[key] = make(map[string]struct{})
	}
	b.staticFields[key][strings.ToLower(fieldName)] = struct{}{}
}

func (b *sourceGraphBuilder) recordStaticWork(className, file, source string, r diagnostic.Range, origin string) {
	if r.Start.Offset < 0 || r.End.Offset <= r.Start.Offset || r.End.Offset > len(source) {
		return
	}
	text := source[r.Start.Offset:r.End.Offset]
	for _, work := range sourceStaticWorks(source, text, r.Start.Offset) {
		init := b.staticInit(className, file, work.line)
		op := b.graph.AddNode(Node{
			Kind:      work.kind,
			Name:      work.name,
			File:      file,
			Line:      work.line,
			Namespace: b.namespaceForClass(className),
			Operation: work.name,
		})
		b.graph.AddEdge(init.id, op, EdgeExecutes)
		b.graph.AddEvidence(init.id, Evidence{Kind: "static", Message: "heavy static initializer", Value: origin})
		b.graph.AddEvidence(init.id, Evidence{Kind: "static", Message: work.message, Value: work.name})
		b.graph.AddEvidence(op, Evidence{Kind: "static", Message: work.message, Value: work.name})
		b.graph.AddResourceRisk(init.id, work.risk)
		b.graph.AddResourceRisk(op, work.risk)
	}
}

func (b *sourceGraphBuilder) staticInit(className, file string, line int) sourceStaticInitFact {
	key := sourceTypeKey(className)
	if existing, ok := b.staticInits[key]; ok {
		return existing
	}
	init := sourceStaticInitFact{
		className: className,
		file:      filepath.ToSlash(file),
		line:      line,
	}
	init.id = b.graph.AddNode(Node{
		Kind:      NodeStaticInit,
		Name:      className + ".<static_init>",
		File:      file,
		Line:      line,
		Namespace: b.namespaceForClass(className),
	})
	b.staticInits[key] = init
	return init
}

func (b *sourceGraphBuilder) addStaticFirstTouchEdges() {
	keys := make([]string, 0, len(b.staticInits))
	for key := range b.staticInits {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, method := range b.methods {
		text := b.methodSource(method)
		if text == "" {
			continue
		}
		for _, classKey := range keys {
			init := b.staticInits[classKey]
			if sourceTypeKey(method.className) == classKey {
				continue
			}
			fields := b.staticFields[classKey]
			if len(fields) == 0 {
				continue
			}
			className := shortTypeName(init.className)
			for field := range fields {
				if !sourceContainsStaticFieldRead(text, className, field) {
					continue
				}
				b.graph.AddEdge(method.id, init.id, EdgeCalls)
				b.graph.AddEvidence(init.id, Evidence{Kind: "static", Message: "first touch via static field", Value: className + "." + field})
				break
			}
		}
	}
}

func (b *sourceGraphBuilder) methodByLine(file string, line int) *sourceMethodFact {
	file = filepath.ToSlash(file)
	var best *sourceMethodFact
	bestSpan := int(^uint(0) >> 1)
	for _, method := range b.methods {
		if filepath.ToSlash(method.file) != file {
			continue
		}
		if method.startLine <= 0 || method.endLine <= 0 || line < method.startLine || line > method.endLine {
			continue
		}
		span := method.endLine - method.startLine
		if best == nil || span < bestSpan {
			best = method
			bestSpan = span
		}
	}
	return best
}

func (b *sourceGraphBuilder) sourceForFile(file string) string {
	file = filepath.ToSlash(file)
	if source, ok := b.sourceByFile[file]; ok {
		return source
	}
	data, err := os.ReadFile(file)
	if err != nil {
		b.sourceByFile[file] = ""
		return ""
	}
	source := string(data)
	b.sourceByFile[file] = source
	return source
}

func (b *sourceGraphBuilder) methodSource(method *sourceMethodFact) string {
	source := b.sourceForFile(method.file)
	if source == "" || method.start < 0 || method.end <= method.start || method.end > len(source) {
		return ""
	}
	return source[method.start:method.end]
}

func (b *sourceGraphBuilder) sourceLine(file string, line int) string {
	if line <= 0 {
		return ""
	}
	source := b.sourceForFile(file)
	if source == "" {
		return ""
	}
	currentLine := 1
	start := 0
	for i := 0; i <= len(source); i++ {
		if i < len(source) && source[i] != '\n' {
			continue
		}
		if currentLine == line {
			return source[start:i]
		}
		currentLine++
		start = i + 1
	}
	return ""
}

func (b *sourceGraphBuilder) namespaceForClass(className string) string {
	if typ, ok := b.typeSymbols[sourceTypeKey(className)]; ok {
		return typ.Namespace
	}
	if typ, ok := b.typeSymbols[sourceTypeKey(shortTypeName(className))]; ok {
		return typ.Namespace
	}
	return b.index.Project.Namespace
}

func (b *sourceGraphBuilder) entryKindForMethod(className string, decl apexast.Declaration) (EntryKind, bool) {
	if sourceHasAnnotation(decl.Modifiers, "AuraEnabled") {
		return EntryAura, true
	}
	if sourceHasAnnotation(decl.Modifiers, "InvocableMethod") {
		return EntryInvocable, true
	}
	if sourceHasAnnotation(decl.Modifiers, "future") {
		return EntryFuture, true
	}
	flags := asyncTypeFlagsFromName(className, collectAsyncTypeFlags(b.index))
	switch {
	case flags.queueable && strings.EqualFold(decl.Name, "execute"):
		return EntryQueueable, true
	case flags.batchable && (strings.EqualFold(decl.Name, "start") || strings.EqualFold(decl.Name, "execute") || strings.EqualFold(decl.Name, "finish")):
		return EntryBatch, true
	case flags.schedulable && strings.EqualFold(decl.Name, "execute"):
		return EntrySchedulable, true
	default:
		return "", false
	}
}

func (b *sourceGraphBuilder) methodLabel(id NodeID) string {
	if method, ok := b.methodsByID[id]; ok {
		return method.className + "." + method.methodName
	}
	return ""
}

type sourceStaticWork struct {
	kind    NodeKind
	name    string
	line    int
	message string
	risk    ResourceRisk
}

func sourceStaticWorks(source, text string, baseOffset int) []sourceStaticWork {
	var out []sourceStaticWork
	addMatches := func(re *regexp.Regexp, kind NodeKind, message string, risk ResourceRisk) {
		for _, match := range re.FindAllStringIndex(text, -1) {
			name := strings.TrimSpace(text[match[0]:match[1]])
			name = strings.TrimSuffix(name, "(")
			out = append(out, sourceStaticWork{
				kind:    kind,
				name:    name,
				line:    lineAt(source, baseOffset+match[0]),
				message: message,
				risk:    risk,
			})
		}
	}
	addMatches(sourceGlobalDescribeRe, NodeDescribe, "Schema.getGlobalDescribe in static initializer", ResourceRisk{CPU: true, Heap: true, SharedLimit: true})
	addMatches(sourceDescribeSObjectsRe, NodeDescribe, "Schema.describeSObjects in static initializer", ResourceRisk{CPU: true, Heap: true, SharedLimit: true})
	addMatches(sourceGetAllRe, NodeDescribe, "metadata getAll in static initializer", ResourceRisk{CPU: true, Heap: true, SharedLimit: true})
	for _, match := range sourceSOQLRe.FindAllStringIndex(text, -1) {
		name := strings.TrimSpace(text[match[0]:match[1]])
		out = append(out, sourceStaticWork{
			kind:    NodeSOQL,
			name:    sourceOperationName(NodeSOQL, name),
			line:    lineAt(source, baseOffset+match[0]),
			message: "SOQL in static initializer",
			risk:    ResourceRisk{CPU: true, Heap: true, DBTime: true, DBRows: true, SharedLimit: true},
		})
	}
	return out
}

func sourceLocalTypes(source string, decl apexast.Declaration) map[string]string {
	out := make(map[string]string)
	for _, param := range decl.Parameters {
		if param.Name != "" && param.Type != "" {
			out[strings.ToLower(param.Name)] = sourceCleanType(param.Type)
		}
	}
	var texts []string
	if decl.Range.Start.Offset >= 0 && decl.Range.End.Offset > decl.Range.Start.Offset && decl.Range.End.Offset <= len(source) {
		texts = append(texts, source[decl.Range.Start.Offset:decl.Range.End.Offset])
	}
	if lineText := sourceLineRangeText(source, decl.Range.Start.Line, decl.Range.End.Line); lineText != "" {
		texts = append(texts, lineText)
	}
	for _, text := range texts {
		for _, match := range sourceVarDeclRe.FindAllStringSubmatch(text, -1) {
			if len(match) < 3 {
				continue
			}
			typeName := sourceCleanType(match[1])
			name := strings.ToLower(strings.TrimSpace(match[2]))
			if sourceIsDMLKeyword(typeName) {
				continue
			}
			if typeName != "" && name != "" {
				out[name] = typeName
			}
		}
	}
	return out
}

func sourceParamTypes(decl apexast.Declaration) []string {
	out := make([]string, 0, len(decl.Parameters))
	for _, param := range decl.Parameters {
		out = append(out, sourceCleanType(param.Type))
	}
	return out
}

func sourceCleanType(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	value = strings.ReplaceAll(value, " <", "<")
	value = strings.ReplaceAll(value, "< ", "<")
	value = strings.ReplaceAll(value, " >", ">")
	value = strings.ReplaceAll(value, "> ", ">")
	value = strings.ReplaceAll(value, " ,", ",")
	value = strings.ReplaceAll(value, ", ", ",")
	return strings.TrimSpace(value)
}

func sourceCallSignatureFromText(method *sourceMethodFact, text string) sourceCallSignature {
	args, ok := sourceInvocationArgs(text)
	if !ok {
		return sourceCallSignature{argCount: -1}
	}
	types := make([]string, 0, len(args))
	for _, arg := range args {
		types = append(types, sourceExpressionType(method, arg))
	}
	return sourceCallSignature{argCount: len(args), argTypes: types}
}

func sourceInvocationArgs(text string) ([]string, bool) {
	start := strings.IndexByte(text, '(')
	end := strings.LastIndexByte(text, ')')
	if start < 0 || end < start {
		return nil, false
	}
	body := strings.TrimSpace(text[start+1 : end])
	if body == "" {
		return nil, true
	}
	var args []string
	depth := 0
	inString := false
	argStart := 0
	for i := 0; i < len(body); i++ {
		ch := body[i]
		if inString {
			if ch == '\'' {
				if i+1 < len(body) && body[i+1] == '\'' {
					i++
					continue
				}
				inString = false
			}
			continue
		}
		switch ch {
		case '\'':
			inString = true
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(body[argStart:i]))
				argStart = i + 1
			}
		}
	}
	args = append(args, strings.TrimSpace(body[argStart:]))
	return args, true
}

func sourceExpressionType(method *sourceMethodFact, expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" || method == nil {
		return ""
	}
	if strings.HasPrefix(expr, "'") {
		return "String"
	}
	if sourceIntegerLiteral(expr) {
		return "Integer"
	}
	if match := regexp.MustCompile(`(?i)\bnew\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?(?:<[^>]+>)?)`).FindStringSubmatch(expr); len(match) >= 2 {
		return sourceCleanType(match[1])
	}
	identifier := expr
	if dot := strings.IndexByte(identifier, '.'); dot >= 0 {
		return ""
	}
	identifier = strings.Trim(identifier, "() ")
	return method.localTypes[strings.ToLower(identifier)]
}

func sourceIntegerLiteral(expr string) bool {
	if expr == "" {
		return false
	}
	for _, ch := range expr {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func sourceIsDMLKeyword(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "insert", "update", "upsert", "delete", "undelete", "merge":
		return true
	default:
		return false
	}
}

func selectSourceMethodCandidate(candidates []*sourceMethodFact, call sourceCallSignature) *sourceMethodFact {
	if len(candidates) == 0 {
		return nil
	}
	ordered := append([]*sourceMethodFact(nil), candidates...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].file != ordered[j].file {
			return ordered[i].file < ordered[j].file
		}
		return ordered[i].startLine < ordered[j].startLine
	})
	if call.argCount < 0 {
		return ordered[0]
	}
	var countMatches []*sourceMethodFact
	for _, candidate := range ordered {
		if len(candidate.paramTypes) == call.argCount {
			countMatches = append(countMatches, candidate)
		}
	}
	if len(countMatches) == 0 {
		return nil
	}
	if sourceKnownTypeCount(call.argTypes) == 0 {
		return countMatches[0]
	}
	var best *sourceMethodFact
	bestScore := -1
	for _, candidate := range countMatches {
		score, ok := sourceMethodTypeMatchScore(candidate.paramTypes, call.argTypes)
		if !ok {
			continue
		}
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func sourceKnownTypeCount(types []string) int {
	count := 0
	for _, typ := range types {
		if strings.TrimSpace(typ) != "" {
			count++
		}
	}
	return count
}

func sourceMethodTypeMatchScore(params, args []string) (int, bool) {
	if len(params) != len(args) {
		return 0, false
	}
	score := 0
	for i := range args {
		arg := sourceComparableType(args[i])
		if arg == "" {
			continue
		}
		param := sourceComparableType(params[i])
		if param == "" || param != arg {
			return 0, false
		}
		score++
	}
	return score, true
}

func sourceComparableType(value string) string {
	value = sourceCleanType(value)
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "<") {
		return strings.ToLower(strings.ReplaceAll(value, " ", ""))
	}
	return strings.ToLower(shortTypeName(value))
}

func sourceDMLObjectNames(method *sourceMethodFact, text string) []string {
	var out []string
	addFromTarget := func(target string) {
		target = strings.TrimSpace(target)
		if sourceLooksLikeObjectName(target) {
			out = append(out, target)
			return
		}
		if method == nil {
			return
		}
		if object := sourceObjectNameFromType(method.localTypes[strings.ToLower(target)]); object != "" {
			out = append(out, object)
		}
	}
	for _, match := range sourceDMLTargetRe.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			addFromTarget(match[1])
		}
	}
	if strings.Contains(strings.ToLower(text), "database.") {
		args, ok := sourceInvocationArgs(text)
		if ok && len(args) > 0 {
			addFromTarget(args[0])
		}
	}
	return dedupeStrings(out)
}

func sourceObjectNameFromType(typeName string) string {
	typeName = sourceCleanType(typeName)
	if inner := sourceCollectionInnerType(typeName); inner != "" {
		typeName = inner
	}
	typeName = shortTypeName(typeName)
	if sourceLooksLikeObjectName(typeName) {
		return typeName
	}
	return ""
}

func sourceCollectionInnerType(typeName string) string {
	lower := strings.ToLower(typeName)
	for _, prefix := range []string{"list<", "set<"} {
		if strings.HasPrefix(lower, prefix) && strings.HasSuffix(typeName, ">") {
			return strings.TrimSpace(typeName[len(prefix) : len(typeName)-1])
		}
	}
	return ""
}

func sourceLineRangeText(source string, startLine, endLine int) string {
	if source == "" || startLine <= 0 || endLine < startLine {
		return ""
	}
	currentLine := 1
	start := -1
	for i := 0; i <= len(source); i++ {
		if currentLine == startLine && start < 0 {
			start = i
		}
		if i < len(source) && source[i] != '\n' {
			continue
		}
		if currentLine == endLine {
			if start >= 0 {
				return source[start:i]
			}
			return ""
		}
		currentLine++
	}
	if start >= 0 {
		return source[start:]
	}
	return ""
}

func sourceOperationName(kind NodeKind, text string) string {
	text = strings.TrimSpace(text)
	if kind == NodeSOQL && strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 160 {
		text = text[:160]
	}
	return text
}

func sourceMethodKeys(className, methodName string) []string {
	return sourceDedupeStrings([]string{
		sourceMethodKey(className, methodName),
		sourceMethodKey(shortTypeName(className), methodName),
	})
}

func sourceDedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := values[:0]
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sourceMethodKey(className, methodName string) string {
	return sourceTypeKey(className) + "." + strings.ToLower(strings.TrimSpace(methodName))
}

func sourceTypeKey(className string) string {
	className = strings.TrimSpace(className)
	className = strings.TrimPrefix(className, ".")
	return strings.ToLower(className)
}

func sourceHasModifier(modifiers []string, expected string) bool {
	for _, modifier := range modifiers {
		if strings.EqualFold(strings.TrimSpace(modifier), expected) {
			return true
		}
	}
	return false
}

func sourceHasAnnotation(modifiers []string, expected string) bool {
	expected = strings.ToLower(strings.TrimPrefix(expected, "@"))
	for _, modifier := range modifiers {
		normalized := strings.ToLower(strings.TrimSpace(modifier))
		normalized = strings.TrimPrefix(normalized, "@")
		if i := strings.IndexByte(normalized, '('); i >= 0 {
			normalized = normalized[:i]
		}
		if normalized == expected {
			return true
		}
	}
	return false
}

func sourceReceiverIsType(segments []string, typeName string) bool {
	return len(segments) > 0 && strings.EqualFold(segments[0], typeName)
}

func sourceReceiverContains(segments []string, value string) bool {
	for _, segment := range segments {
		if strings.EqualFold(segment, value) {
			return true
		}
	}
	return false
}

func sourceContainsStaticFieldRead(text, className, fieldName string) bool {
	if className == "" || fieldName == "" {
		return false
	}
	pattern := `(?i)\b` + regexp.QuoteMeta(className) + `\s*\.\s*` + regexp.QuoteMeta(fieldName) + `\b`
	return regexp.MustCompile(pattern).FindStringIndex(text) != nil
}
