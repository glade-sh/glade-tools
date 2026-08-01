package surfaceledger

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCB56CurrentProductRowsReflectLunaMaxRuntimeSymbols(t *testing.T) {
	byID := rowsByID(BuildGladeSnapshot())

	wantSupported := []string{
		ApexMemberID("ApexPages", "Action", "invoke", []string{}),
		ApexMemberID("Auth", "JWT", "getAdditionalClaims", []string{}),
		ApexMemberID("Auth", "JWT", "getAud", []string{}),
		ApexMemberID("Auth", "JWT", "getIss", []string{}),
		ApexMemberID("Auth", "JWT", "getNbfClockSkew", []string{}),
		ApexMemberID("Auth", "JWT", "getSub", []string{}),
		ApexMemberID("Auth", "JWT", "getValidityLength", []string{}),
		ApexMemberID("Auth", "JWT", "setAdditionalClaims", []string{"Map<String,Object>"}),
		ApexMemberID("Auth", "JWT", "setAud", []string{"String"}),
		ApexMemberID("Auth", "JWT", "setIss", []string{"String"}),
		ApexMemberID("Auth", "JWT", "setNbfClockSkew", []string{"Integer"}),
		ApexMemberID("Auth", "JWT", "setSub", []string{"String"}),
		ApexMemberID("Auth", "JWT", "setValidityLength", []string{"Integer"}),
		ApexMemberID("Auth", "JWT", "toJSONString", []string{}),
		ApexMemberID("Auth", "JWTUtil", "parseJWTFromStringWithoutValidation", []string{"String"}),
		ApexMemberID("System", "InstallHandler", "onInstall", []string{"InstallContext"}),
		ApexMemberID("System", "UninstallContext", "organizationId", []string{}),
		ApexMemberID("System", "UninstallHandler", "onUninstall", []string{"UninstallContext"}),
		ApexMemberID("Messaging", "ActionableNotification.Builder", "withActionIdentifier", []string{"String"}),
		ApexMemberID("Messaging", "ActionableNotification.Builder", "withTargetId", []string{"String"}),
		ApexMemberID("Messaging", "ActionableNotification.Builder", "withTargetPageRef", []string{"String"}),
		"apex:Messaging.SingleEmailMessage.customHeaders",
		ApexMemberID("Messaging", "SingleEmailMessage", "getCustomHeaders", []string{}),
		ApexMemberID("Messaging", "SingleEmailMessage", "setCustomHeaders", []string{"Map<String,String>"}),
	}
	for _, id := range wantSupported {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("current product symbol is missing from Glade snapshot: %s", id)
		}
		if row.GladeShape == ShapeAbsent || row.GladeBehavior != BehaviorSupported {
			t.Errorf("%s shape/behavior = %s/%s, want present/supported", id, row.GladeShape, row.GladeBehavior)
		}
	}

	jwtType, ok := byID[ApexTypeID("Auth", "JWT")]
	if !ok {
		t.Fatal("current Auth.JWT type is missing from Glade snapshot")
	}
	if jwtType.GladeBehavior != BehaviorSupported {
		t.Fatalf("Auth.JWT type behavior = %s, want %s", jwtType.GladeBehavior, BehaviorSupported)
	}
}

func TestCB56NonCanonicalRowsDoNotBecomeLedgerObligations(t *testing.T) {
	nonCanonical := []string{
		"apex:System.Assert.areEqual(Object,Object,Object)",
		"apex:System.Assert.areNotEqual(Object,Object,Object)",
		"apex:System.Assert.isTrue(Boolean,Object)",
		"apex:System.Assert.isFalse(Boolean,Object)",
		"apex:System.Assert.isNull(Object,Object)",
		"apex:System.Assert.isNotNull(Object,Object)",
		"apex:System.Assert.fail(Object)",
		"apex:System.Http.send(Object)",
		"apex:Messaging.ActionableNotification.Builder.withActionIdentifier",
		"apex:Messaging.ActionableNotification.Builder.withTargetId",
		"apex:Messaging.ActionableNotification.Builder.withTargetPageRef",
		"apex:Messaging.CustomNotification.CustomNotification(String,String,String,String,String,String,String)",
		"apex:Messaging.CustomNotification.setActionGroup(String)",
		"apex:System.IntegrationTest.clone()",
		"apex:Approval.*",
		"apex:Search.SuggestionOption.setFilter(Search.KnowledegeSuggestionFilter)",
		"apex:System.BusinessHours malformed local holiday metadata",
		"apex:System.InvalidParameterValueException constructors",
		"apex:System.Limits.get*",
		"apex:System.NoAccessException constructors",
		"apex:System.NoDataFoundException constructors",
		"apex:System.NullPointerException constructors",
		"apex:System.PageReference(partialURL)",
		"apex:System.Search.query / SOSL FIND",
		"apex:System.unimplemented platform/stdlib calls",
	}
	rows := make([]SurfaceLedgerRow, 0, len(nonCanonical)+1)
	for _, id := range nonCanonical {
		if _, ok := nonCanonicalGeneratedSurfaceIDs[id]; !ok {
			t.Errorf("CB56 noncanonical identity is not registered: %s", id)
		}
		rows = append(rows, SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductApex,
			Area:          AreaRuntime,
			Kind:          KindMethod,
			GladeShape:    ShapeSignatureKnown,
			GladeBehavior: BehaviorUnsupported,
		})
	}
	canonical := apexMemberRow(
		ApexMemberID("System", "Assert", "areEqual", []string{"Object", "Object", "String"}),
		"System", "Assert", "areEqual",
	)
	rows = append(rows, canonical)

	ledger := Merge(rows, nil, nil, nil)
	byID := rowsByID(ledger.Rows)
	for _, id := range nonCanonical {
		if _, ok := byID[id]; ok {
			t.Errorf("noncanonical identity remains in merged ledger: %s", id)
		}
	}
	if _, ok := byID[canonical.SurfaceID]; !ok {
		t.Fatalf("canonical Assert overload was filtered with stale identities: %s", canonical.SurfaceID)
	}
}

func TestCB56HostedPolicyCoversOnlyDeclaredServiceEffects(t *testing.T) {
	policy, err := LoadSupportPolicy(filepath.Join("..", "..", "docs", "fixtures", "apex-local-support-policy.json"))
	if err != nil {
		t.Fatal(err)
	}

	hostedIDs := []string{
		"apex:BusinessRule.CalculationMatrixMigrationService.migrate(List<String>,String)",
		"apex:BusinessRule.CalculationMatrixMigrationService.migrate(String,String)",
		"apex:BusinessRule.CalculationProcedureMigrationService.migrate(List<String>,String)",
		"apex:BusinessRule.CalculationProcedureMigrationService.migrate(String,String)",
		"apex:BusinessRule.DecisionMatrixRowMigratorService.migrate(String)",
		"apex:Context.IndustriesContext.deleteContext(Map<String,Object>)",
		"apex:Context.IndustriesContext.deleteRecords(Map<String,Object>)",
		"apex:Context.IndustriesContext.evictContextDefinition(Map<String,Object>)",
		"apex:System.Crypto.signXML(String,Dom.XmlNode,String,String)",
		"apex:System.Crypto.signXml(String,dom.XmlNode,String,String,dom.XmlNode)",
		"apex:UserProvisioning.UserProvisioningPlugin.invoke(Process.PluginRequest)",
		"apex:applauncher.AppLauncherSetupReordererController.getModel()",
		"apex:applauncher.AppLauncherSetupReordererController.saveOrder(String)",
		"apex:applauncher.ChangePasswordController.changePassowrd(String,String,String)",
		"apex:applauncher.ChangePasswordController.changePassword(String,String,String,Boolean)",
		"apex:applauncher.ChangePasswordController.getPasswordPolicyStatement()",
		"apex:applauncher.ForgotPasswordController.forgotPassword(String,String)",
		"apex:applauncher.ForgotPasswordController.setExperienceId(String)",
		"apex:applauncher.LoginFormController.login(String,String,String)",
		"apex:applauncher.SelfRegisterController.selfRegister(String,String,String,String,String,String,String,String,String,Boolean)",
		"apex:applauncher.SocialLoginController.handleIdp()",
		"apex:functions.Function.get(String)",
		"apex:functions.Function.get(String,String)",
		"apex:functions.Function.invoke(String)",
		"apex:functions.Function.invoke(String,functions.FunctionCallback)",
		"apex:functions.FunctionCallback.handleResponse(functions.FunctionInvocation)",
		"apex:functions.FunctionInvocable.invoke(String,functions.FunctionContext)",
		"apex:pref.PreferenceCenterApexHandler.load(pref_center.LoadParameters,pref_center.LoadFormData,pref_center.ValidationResult)",
		"apex:pref.PreferenceCenterApexHandler.submit(pref_center.SubmitParameters,pref_center.SubmitFormData,pref_center.ValidationResult)",
		"unknown:salesforce_app_limits_platform_soslsoql",
		"unknown:supported_soql",
		"unknown:unsupported_soql_statements",
		"apex:System.Exact Salesforce governor accounting profiles",
		"apex:System.Messaging.renderStoredEmailTemplate hosted usage mutation",
		"apex:System.Messaging.sendEmail delivery transport",
		"apex:System.PageReference.getContent and getContentAsPDF",
		"apex:System.PackageBundleService",
		"apex:System.PackageBundleService.PackageBundleService()",
		"apex:System.PackageBundleService.clone()",
		"apex:System.PackageBundleService.getBundleVersionComponents(String,String)",
		"apex:System.PackageBundleService.getBundleVersions(String,String)",
		"apex:System.PackageBundleService.getBundles(String)",
		"apex:System.PackageBundleService.getBundlesWithVersionsAndComponents(String)",
		"apex:System.System.enqueueJob hosted wall-clock queue scheduling",
		"apex:System.System.runAs package install and license validation",
		"apex:System.Test.getExternalService live service execution",
		"apex:System.Test.loadData packaged resource and relationship external-ID expansion",
		"apex:System.Test.startTest and stopTest hosted service accounting",
		"apex:System.Type.forName hosted package namespace reflection",
	}
	rows := make([]SurfaceLedgerRow, 0, len(hostedIDs))
	for _, id := range hostedIDs {
		row := SurfaceLedgerRow{
			SurfaceID:     id,
			Product:       ProductApex,
			Area:          AreaRuntime,
			GladeShape:    ShapeSignatureKnown,
			GladeBehavior: BehaviorUnsupported,
		}
		if strings.HasPrefix(id, "apex:") {
			fillFromApexID(&row)
		}
		rows = append(rows, row)
	}
	rows = appendPolicyExceptionRows(rows, policy.Rules)
	profile := ComputeSupportProfile(rows, policy, nil)
	if len(profile.ValidationErrors) != 0 {
		t.Fatalf("hosted policy test produced validation errors: %v", profile.ValidationErrors)
	}
	byID := make(map[string]SupportProfileRow, len(profile.Rows))
	for _, row := range profile.Rows {
		byID[row.SurfaceID] = row
	}
	for _, id := range hostedIDs {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("hosted policy row is missing: %s", id)
		}
		if row.Disposition != DispositionHostedDeferred || row.GapClass != "" {
			t.Errorf("%s disposition/gap = %s/%s, want hosted-deferred/closed", id, row.Disposition, row.GapClass)
		}
	}

	localIDs := map[string]SupportDisposition{
		"apex:Auth.JWT.getAud()": DispositionDeterministicMockRequired,
		"apex:Auth.JWTUtil.parseJWTFromStringWithoutValidation(String)":          DispositionDeterministicMockRequired,
		"apex:Messaging.SingleEmailMessage.setCustomHeaders(Map<String,String>)": DispositionDeterministicMockRequired,
		"apex:System.Crypto.decryptWithManagedIV(String,Blob,Blob,Blob)":          DispositionLocalRuntimeRequired,
		"apex:System.Crypto.encryptWithManagedIV(String,Blob,Blob,Blob)":          DispositionLocalRuntimeRequired,
		"apex:System.Http.send(HttpRequest)":                                     DispositionDeterministicMockRequired,
		"apex:Context.IndustriesContext.getContext(Map<String,Object>)":          DispositionDeterministicMockRequired,
		"apex:applauncher.AppMenu.setOrgSortOrder(List<Id>)":                     DispositionDeterministicMockRequired,
	}
	localRows := make([]SurfaceLedgerRow, 0, len(localIDs))
	for id := range localIDs {
		row := SurfaceLedgerRow{SurfaceID: id, Product: ProductApex, Area: AreaRuntime, GladeShape: ShapeSignatureKnown, GladeBehavior: BehaviorSupported, Evidence: EvidenceFixture}
		fillFromApexID(&row)
		localRows = append(localRows, row)
	}
	localRows = appendPolicyExceptionRows(localRows, policy.Rules)
	localProfile := ComputeSupportProfile(localRows, policy, nil)
	if len(localProfile.ValidationErrors) != 0 {
		t.Fatalf("local boundary policy test produced validation errors: %v", localProfile.ValidationErrors)
	}
	localByID := make(map[string]SupportProfileRow, len(localProfile.Rows))
	for _, row := range localProfile.Rows {
		localByID[row.SurfaceID] = row
	}
	for id, want := range localIDs {
		row, ok := localByID[id]
		if !ok {
			t.Fatalf("local boundary policy row is missing: %s", id)
		}
		if row.Disposition != want {
			t.Errorf("%s disposition = %s, want %s", id, row.Disposition, want)
		}
	}
}
