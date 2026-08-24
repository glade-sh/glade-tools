package toolcli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/glade-sh/glade/tools/internal/corpusassurance"
	"github.com/glade-sh/glade/tools/internal/releasecontract"
)

func runCorpusAssuranceOrchestrator(ctx context.Context, args []string, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 || isHelpArg(args[0]) {
		_, err := fmt.Fprintln(w, "glade-tools corpus assurance orchestrator <plan|init|enqueue|status|lease|heartbeat|hub-observe|reserve|receipt|worker-once|raw-ingest|raw-accept|raw-abort-observe|raw-abort-accept|ssh-dispatch|ssh-fetch|worker-transfer|cleanup-takeover|cleanup-claim>")
		return err
	}
	switch args[0] {
	case "worker-once":
		flags := orchestratorFlags("worker-once")
		planPath, leasePath := flags.String("plan", "", ""), flags.String("lease", "", "")
		planSHA, leaseSHA := flags.String("plan-sha256", "", ""), flags.String("lease-sha256", "", "")
		bundlePath, devHub, targetOrg := flags.String("bundle", "", ""), flags.String("dev-hub", "", ""), flags.String("target-org", "", "")
		sfBin, outputRoot := flags.String("sf-bin", "", ""), flags.String("output-root", "", "")
		if err := rejectDuplicateAssuranceFlags(args[1:], nil); err != nil {
			return err
		}
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*planPath, *planSHA, *leasePath, *leaseSHA, *bundlePath, *devHub, *targetOrg, *sfBin, *outputRoot); err != nil {
			return err
		}
		for _, path := range []string{*planPath, *leasePath, *bundlePath, *sfBin, *outputRoot} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return errors.New("absolute clean worker-once paths are required")
			}
		}
		var plan corpusassurance.OrchestratorCampaignPlan
		planBytes, err := readOrchestratorJSONBytes(*planPath, &plan)
		if err != nil {
			return err
		}
		actualPlanSHA := fmt.Sprintf("%x", sha256.Sum256(planBytes))
		if actualPlanSHA != *planSHA {
			return errors.New("worker plan does not match dispatched hash")
		}
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executing worker: %w", err)
		}
		executingSHA, err := sha256File(executable)
		if err != nil || validateOrchestratorWorkerExecutable(plan.Definition, *bundlePath, executingSHA) != nil {
			return errors.New("executing worker does not match sealed tools")
		}
		var lease corpusassurance.OrchestratorLease
		leaseBytes, err := readOrchestratorJSONBytes(*leasePath, &lease)
		if err != nil {
			return err
		}
		actualLeaseSHA := fmt.Sprintf("%x", sha256.Sum256(leaseBytes))
		if actualLeaseSHA != *leaseSHA {
			return errors.New("worker lease does not match dispatched hash")
		}
		result, err := corpusassurance.RunRawSalesforceShard(corpusassurance.RawSalesforceShardRequest{Plan: plan, Lease: lease, BundlePath: *bundlePath, DevHub: *devHub, TargetOrg: *targetOrg, SFBin: *sfBin, OutputRoot: *outputRoot})
		if err != nil {
			return err
		}
		completion, err := corpusassurance.OrchestratorWorkerOnceCompletionFromRaw(plan, lease, actualPlanSHA, actualLeaseSHA, result)
		if err != nil {
			return err
		}
		return writeOrchestratorOutput(w, completion)
	case "raw-ingest":
		flags := orchestratorFlags("raw-ingest")
		planPath, leasePath, oraclePlanPath := flags.String("plan", "", ""), flags.String("lease", "", ""), flags.String("oracle-plan", "", "")
		rawRoot, packetOutput, output := flags.String("raw-root", "", ""), flags.String("packet-output", "", ""), flags.String("output", "", "")
		if err := rejectDuplicateAssuranceFlags(args[1:], nil); err != nil {
			return err
		}
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*planPath, *leasePath, *oraclePlanPath, *rawRoot, *packetOutput, *output); err != nil {
			return err
		}
		for _, path := range []string{*planPath, *leasePath, *oraclePlanPath, *rawRoot, *packetOutput, *output} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return errors.New("absolute clean raw-ingest paths are required")
			}
		}
		var plan corpusassurance.OrchestratorCampaignPlan
		if err := readOrchestratorJSON(*planPath, &plan); err != nil {
			return fmt.Errorf("read orchestrator plan: %w", err)
		}
		var lease corpusassurance.OrchestratorLease
		if err := readOrchestratorJSON(*leasePath, &lease); err != nil {
			return fmt.Errorf("read orchestrator lease: %w", err)
		}
		if err := validateRawIngestRoot(*rawRoot); err != nil {
			return err
		}
		files := corpusassurance.SalesforceShardFiles{
			ShardPath: filepath.Join(*rawRoot, "SALESFORCE_SHARD.json"), DispatchPath: filepath.Join(*rawRoot, "SALESFORCE_DISPATCH.json"),
			CreationPath: filepath.Join(*rawRoot, "ORG_CREATION.json"), CleanupPath: filepath.Join(*rawRoot, "ORG_CLEANUP.json"), PreflightPath: filepath.Join(*rawRoot, "ORG_PREFLIGHT.json"),
		}
		receipt, err := corpusassurance.CreateOrchestratorSalesforceReconciliation(corpusassurance.OrchestratorSalesforceReconciliationRequest{
			Plan: plan, Lease: lease, OraclePlanPath: *oraclePlanPath, BindingPath: filepath.Join(*rawRoot, "ORCHESTRATOR_BINDING.json"), ShardFiles: files, PacketOutput: *packetOutput, OutputPath: *output,
		})
		if err != nil {
			return err
		}
		return writeOrchestratorOutput(w, receipt)
	case "raw-accept":
		flags := orchestratorFlags("raw-accept")
		database, planPath, leasePath := flags.String("db", "", ""), flags.String("plan", "", ""), flags.String("lease", "", "")
		allocation, sshPath, receiptPath := flags.String("allocation", "", ""), flags.String("ssh-receipt", "", ""), flags.String("receipt", "", "")
		packetPath, outputPath := flags.String("packet", "", ""), flags.String("output", "", "")
		if err := rejectDuplicateAssuranceFlags(args[1:], nil); err != nil {
			return err
		}
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *planPath, *leasePath, *allocation, *sshPath, *receiptPath, *packetPath, *outputPath); err != nil {
			return err
		}
		for _, path := range []string{*database, *planPath, *leasePath, *sshPath, *receiptPath, *packetPath, *outputPath} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return errors.New("absolute clean raw-accept paths are required")
			}
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			var planValue corpusassurance.OrchestratorCampaignPlan
			planBytes, err := readOrchestratorJSONBytes(*planPath, &planValue)
			if err != nil {
				return fmt.Errorf("read orchestrator plan: %w", err)
			}
			var leaseValue corpusassurance.OrchestratorLease
			leaseBytes, err := readOrchestratorJSONBytes(*leasePath, &leaseValue)
			if err != nil {
				return fmt.Errorf("read orchestrator lease: %w", err)
			}
			var sshReceipt corpusassurance.OrchestratorSSHDispatchReceipt
			sshBytes, err := readOrchestratorJSONBytes(*sshPath, &sshReceipt)
			if err != nil {
				return fmt.Errorf("read SSH receipt: %w", err)
			}
			accepted, err := corpusassurance.AcceptOrchestratorRawCanary(corpusassurance.OrchestratorRawCanaryRequest{Coordinator: orchestrator, Plan: planValue, Lease: leaseValue, PlanSHA256: fmt.Sprintf("%x", sha256.Sum256(planBytes)), LeaseSHA256: fmt.Sprintf("%x", sha256.Sum256(leaseBytes)), SSHReceiptSHA256: fmt.Sprintf("%x", sha256.Sum256(sshBytes)), AllocationAlias: *allocation, SSHReceipt: sshReceipt, ReceiptPath: *receiptPath, PacketPath: *packetPath, OutputPath: *outputPath})
			if err != nil {
				return err
			}
			return writeOrchestratorOutput(w, accepted)
		})
	case "raw-abort-observe":
		flags := orchestratorFlags("raw-abort-observe")
		planPath, leasePath := flags.String("plan", "", ""), flags.String("lease", "", "")
		sshPath, bundlePath := flags.String("ssh-receipt", "", ""), flags.String("bundle", "", "")
		allocation, sfBin := flags.String("allocation", "", ""), flags.String("sf-bin", "", "")
		rawRoot, outputPath := flags.String("raw-root", "", ""), flags.String("output", "", "")
		if err := rejectDuplicateAssuranceFlags(args[1:], nil); err != nil {
			return err
		}
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*planPath, *leasePath, *sshPath, *bundlePath, *allocation, *sfBin, *rawRoot, *outputPath); err != nil {
			return err
		}
		var plan corpusassurance.OrchestratorCampaignPlan
		planBytes, err := readOrchestratorJSONBytes(*planPath, &plan)
		if err != nil {
			return fmt.Errorf("read orchestrator plan: %w", err)
		}
		var lease corpusassurance.OrchestratorLease
		leaseBytes, err := readOrchestratorJSONBytes(*leasePath, &lease)
		if err != nil {
			return fmt.Errorf("read orchestrator lease: %w", err)
		}
		var sshReceipt corpusassurance.OrchestratorSSHDispatchReceipt
		sshBytes, err := readOrchestratorJSONBytes(*sshPath, &sshReceipt)
		if err != nil {
			return fmt.Errorf("read SSH receipt: %w", err)
		}
		observed, err := corpusassurance.ObserveOrchestratorRawPrecreationAbort(corpusassurance.OrchestratorRawPrecreationAbortObservationRequest{
			Plan: plan, Lease: lease, PlanSHA256: fmt.Sprintf("%x", sha256.Sum256(planBytes)), LeaseSHA256: fmt.Sprintf("%x", sha256.Sum256(leaseBytes)),
			FailedSSHReceipt: sshReceipt, FailedSSHReceiptSHA256: fmt.Sprintf("%x", sha256.Sum256(sshBytes)), BundlePath: *bundlePath,
			RawRoot: *rawRoot, AllocationAlias: *allocation, TargetOrg: *allocation, SFBin: *sfBin, OutputPath: *outputPath,
		})
		if err != nil {
			return err
		}
		return writeOrchestratorOutput(w, observed)
	case "raw-abort-accept":
		flags := orchestratorFlags("raw-abort-accept")
		database, planPath, leasePath := flags.String("db", "", ""), flags.String("plan", "", ""), flags.String("lease", "", "")
		sshPath, allocation := flags.String("ssh-receipt", "", ""), flags.String("allocation", "", "")
		observationPath, outputPath := flags.String("observation", "", ""), flags.String("output", "", "")
		if err := rejectDuplicateAssuranceFlags(args[1:], nil); err != nil {
			return err
		}
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *planPath, *leasePath, *sshPath, *allocation, *observationPath, *outputPath); err != nil {
			return err
		}
		var plan corpusassurance.OrchestratorCampaignPlan
		planBytes, err := readOrchestratorJSONBytes(*planPath, &plan)
		if err != nil {
			return fmt.Errorf("read orchestrator plan: %w", err)
		}
		var lease corpusassurance.OrchestratorLease
		leaseBytes, err := readOrchestratorJSONBytes(*leasePath, &lease)
		if err != nil {
			return fmt.Errorf("read orchestrator lease: %w", err)
		}
		var sshReceipt corpusassurance.OrchestratorSSHDispatchReceipt
		sshBytes, err := readOrchestratorJSONBytes(*sshPath, &sshReceipt)
		if err != nil {
			return fmt.Errorf("read SSH receipt: %w", err)
		}
		observationBytes, err := os.ReadFile(*observationPath)
		if err != nil {
			return fmt.Errorf("read raw abort observation: %w", err)
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			accepted, err := corpusassurance.AcceptOrchestratorRawPrecreationAbort(corpusassurance.OrchestratorRawPrecreationAbortAcceptanceRequest{
				Coordinator: orchestrator, Plan: plan, Lease: lease, PlanSHA256: fmt.Sprintf("%x", sha256.Sum256(planBytes)), LeaseSHA256: fmt.Sprintf("%x", sha256.Sum256(leaseBytes)),
				FailedSSHReceipt: sshReceipt, FailedSSHReceiptSHA256: fmt.Sprintf("%x", sha256.Sum256(sshBytes)), AllocationAlias: *allocation,
				ObservationPath: *observationPath, ObservationSHA256: fmt.Sprintf("%x", sha256.Sum256(observationBytes)), OutputPath: *outputPath,
			})
			if err != nil {
				return err
			}
			return writeOrchestratorOutput(w, accepted)
		})
	case "ssh-dispatch":
		flags := orchestratorFlags("ssh-dispatch")
		database := flags.String("db", "", "")
		host := flags.String("host", "", "")
		workerBin := flags.String("worker-bin", "", "")
		planPath, leasePath := flags.String("plan", "", ""), flags.String("lease", "", "")
		bundlePath, targetOrg := flags.String("bundle", "", ""), flags.String("target-org", "", "")
		sfBin, outputRoot, output := flags.String("sf-bin", "", ""), flags.String("output-root", "", ""), flags.String("output", "", "")
		if err := rejectDuplicateAssuranceFlags(args[1:], nil); err != nil {
			return err
		}
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *host, *workerBin, *planPath, *leasePath, *bundlePath, *targetOrg, *sfBin, *outputRoot, *output); err != nil {
			return err
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			receipt, err := corpusassurance.RunOrchestratorSSHDispatch(corpusassurance.OrchestratorSSHDispatchRequest{Coordinator: orchestrator, Host: *host, WorkerBin: *workerBin, PlanPath: *planPath, LeasePath: *leasePath, BundlePath: *bundlePath, TargetOrg: *targetOrg, SFBin: *sfBin, OutputRoot: *outputRoot, OutputPath: *output})
			return writeOrchestratorSSHResult(w, receipt, err)
		})
	case "ssh-fetch":
		flags := orchestratorFlags("ssh-fetch")
		planPath, leasePath, sshPath := flags.String("plan", "", ""), flags.String("lease", "", ""), flags.String("ssh-receipt", "", "")
		host, workerBin, bundlePath := flags.String("host", "", ""), flags.String("worker-bin", "", ""), flags.String("bundle", "", "")
		devHub, targetOrg, sfBin := flags.String("dev-hub", "", ""), flags.String("target-org", "", ""), flags.String("sf-bin", "", "")
		remoteRoot, rawRoot := flags.String("remote-root", "", ""), flags.String("raw-root", "", "")
		if err := rejectDuplicateAssuranceFlags(args[1:], nil); err != nil {
			return err
		}
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*planPath, *leasePath, *sshPath, *host, *workerBin, *bundlePath, *devHub, *targetOrg, *sfBin, *remoteRoot, *rawRoot); err != nil {
			return err
		}
		for _, path := range []string{*planPath, *leasePath, *sshPath, *workerBin, *bundlePath, *sfBin, *remoteRoot, *rawRoot} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return errors.New("absolute clean ssh-fetch paths are required")
			}
		}
		var plan corpusassurance.OrchestratorCampaignPlan
		if err := readOrchestratorJSON(*planPath, &plan); err != nil {
			return fmt.Errorf("read orchestrator plan: %w", err)
		}
		var lease corpusassurance.OrchestratorLease
		if err := readOrchestratorJSON(*leasePath, &lease); err != nil {
			return fmt.Errorf("read orchestrator lease: %w", err)
		}
		var dispatch corpusassurance.OrchestratorSSHDispatchReceipt
		if err := readOrchestratorJSON(*sshPath, &dispatch); err != nil {
			return fmt.Errorf("read SSH receipt: %w", err)
		}
		receipt, err := corpusassurance.FetchOrchestratorSSHRaw(corpusassurance.OrchestratorSSHRawFetchRequest{Plan: plan, Lease: lease, Dispatch: dispatch, Host: *host, WorkerBin: *workerBin, PlanPath: *planPath, LeasePath: *leasePath, DispatchPath: *sshPath, BundlePath: *bundlePath, DevHub: *devHub, TargetOrg: *targetOrg, SFBin: *sfBin, RemoteRoot: *remoteRoot, LocalRoot: *rawRoot})
		if err != nil {
			return err
		}
		return writeOrchestratorOutput(w, receipt)
	case "worker-transfer":
		flags := orchestratorFlags("worker-transfer")
		planPath, leasePath := flags.String("plan", "", ""), flags.String("lease", "", "")
		sourceBatch, evidenceRoot := flags.String("source-batch", "", ""), flags.String("evidence-root", "", "")
		oraclePlan, output := flags.String("oracle-plan", "", ""), flags.String("output", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*planPath, *leasePath, *sourceBatch, *evidenceRoot, *oraclePlan, *output); err != nil {
			return err
		}
		if !filepath.IsAbs(*output) || filepath.Clean(*output) != *output {
			return errors.New("absolute clean worker transfer output path is required")
		}
		if _, err := os.Lstat(*output); err == nil {
			return errors.New("worker transfer output already exists")
		} else if !os.IsNotExist(err) {
			return err
		}
		var plan corpusassurance.OrchestratorCampaignPlan
		if err := readOrchestratorJSON(*planPath, &plan); err != nil {
			return err
		}
		var lease corpusassurance.OrchestratorLease
		if err := readOrchestratorJSON(*leasePath, &lease); err != nil {
			return err
		}
		transfer, err := corpusassurance.TransferOrchestratorWorkerBatch(corpusassurance.OrchestratorWorkerTransferRequest{
			Plan: plan, Lease: lease, SourceBatchRoot: *sourceBatch, EvidenceRoot: *evidenceRoot, OraclePlanPath: *oraclePlan,
		})
		if err != nil {
			return err
		}
		if err := writeOrchestratorJSON(*output, transfer); err != nil {
			return err
		}
		return writeOrchestratorOutput(w, transfer)
	case "cleanup-takeover":
		flags := orchestratorFlags("cleanup-takeover")
		database, requestPath := flags.String("db", "", ""), flags.String("request", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *requestPath); err != nil {
			return err
		}
		var request corpusassurance.OrchestratorCleanupTakeoverRequest
		if err := readOrchestratorJSON(*requestPath, &request); err != nil {
			return err
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			if err := corpusassurance.RunOrchestratorCleanupTakeover(orchestrator, request); err != nil {
				return err
			}
			return writeOrchestratorOutput(w, map[string]string{"status": "cleanup-closed", "allocation": request.Claim.AllocationAlias})
		})
	case "plan":
		flags := orchestratorFlags("plan")
		campaign, output := flags.String("campaign", "", ""), flags.String("output", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*campaign, *output); err != nil {
			return err
		}
		var definition corpusassurance.OrchestratorCampaignDefinition
		if err := readOrchestratorJSON(*campaign, &definition); err != nil {
			return err
		}
		plan, err := corpusassurance.PlanOrchestratorCampaign(definition)
		if err != nil {
			return err
		}
		if err := writeOrchestratorJSON(*output, plan); err != nil {
			return err
		}
		return writeOrchestratorOutput(w, plan)
	case "init", "enqueue":
		flags := orchestratorFlags(args[0])
		database, planPath := flags.String("db", "", ""), flags.String("plan", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *planPath); err != nil {
			return err
		}
		var plan corpusassurance.OrchestratorCampaignPlan
		if err := readOrchestratorJSON(*planPath, &plan); err != nil {
			return err
		}
		orchestrator, err := corpusassurance.OpenOrchestrator(*database)
		if err != nil {
			return err
		}
		defer orchestrator.Close()
		if args[0] == "init" {
			err = orchestrator.InitCampaign(plan)
		} else {
			err = orchestrator.Enqueue(plan)
		}
		if err != nil {
			return err
		}
		return writeOrchestratorOutput(w, map[string]string{"campaignId": plan.CampaignID, "operation": args[0]})
	case "status":
		flags := orchestratorFlags("status")
		database, campaign := flags.String("db", "", ""), flags.String("campaign", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *campaign); err != nil {
			return err
		}
		orchestrator, err := corpusassurance.OpenOrchestrator(*database)
		if err != nil {
			return err
		}
		defer orchestrator.Close()
		status, err := orchestrator.Status(*campaign)
		if err != nil {
			return err
		}
		return writeOrchestratorOutput(w, status)
	case "lease":
		flags := orchestratorFlags("lease")
		database, campaign, worker := flags.String("db", "", ""), flags.String("campaign", "", ""), flags.String("worker", "", "")
		seconds, output := flags.Int("seconds", 0, ""), flags.String("output", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *campaign, *worker, *output); err != nil || *seconds <= 0 {
			if err != nil {
				return err
			}
			return errors.New("positive lease seconds are required")
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			lease, err := orchestrator.Lease(*campaign, *worker, time.Now().UTC(), time.Duration(*seconds)*time.Second)
			if err != nil {
				return err
			}
			if err := writeOrchestratorJSON(*output, lease); err != nil {
				return err
			}
			return writeOrchestratorOutput(w, lease)
		})
	case "heartbeat":
		flags := orchestratorFlags("heartbeat")
		database, leasePath := flags.String("db", "", ""), flags.String("lease", "", "")
		seconds := flags.Int("seconds", 0, "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *leasePath); err != nil || *seconds <= 0 {
			if err != nil {
				return err
			}
			return errors.New("positive heartbeat seconds are required")
		}
		var lease corpusassurance.OrchestratorLease
		if err := readOrchestratorJSON(*leasePath, &lease); err != nil {
			return err
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			if err := orchestrator.Heartbeat(lease, time.Now().UTC(), time.Duration(*seconds)*time.Second); err != nil {
				return err
			}
			return writeOrchestratorOutput(w, map[string]string{"status": "heartbeat-recorded"})
		})
	case "reserve":
		flags := orchestratorFlags("reserve")
		database, leasePath := flags.String("db", "", ""), flags.String("lease", "", "")
		hub, allocation := flags.String("hub", "", ""), flags.String("allocation", "", "")
		capacity := flags.Int("capacity", 0, "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *leasePath, *hub, *allocation); err != nil || *capacity <= 0 {
			if err != nil {
				return err
			}
			return errors.New("positive hub capacity is required")
		}
		var lease corpusassurance.OrchestratorLease
		if err := readOrchestratorJSON(*leasePath, &lease); err != nil {
			return err
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			if err := orchestrator.SetHubCapacity(*hub, *capacity); err != nil {
				return err
			}
			if err := orchestrator.Reserve(lease, *hub, *allocation, time.Now().UTC()); err != nil {
				return err
			}
			return writeOrchestratorOutput(w, map[string]string{"allocation": *allocation, "status": "reserved"})
		})
	case "hub-observe":
		flags := orchestratorFlags("hub-observe")
		database, observationPath := flags.String("db", "", ""), flags.String("observation", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *observationPath); err != nil {
			return err
		}
		observation, err := corpusassurance.ReadOrchestratorHubObservation(*observationPath)
		if err != nil {
			return err
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			if err := orchestrator.ObserveHub(observation); err != nil {
				return err
			}
			return writeOrchestratorOutput(w, map[string]string{"status": "hub-observed"})
		})
	case "receipt":
		flags := orchestratorFlags("receipt")
		database, leasePath := flags.String("db", "", ""), flags.String("lease", "", "")
		batch, output := flags.String("batch", "", ""), flags.String("output", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *leasePath, *batch, *output); err != nil {
			return err
		}
		var lease corpusassurance.OrchestratorLease
		if err := readOrchestratorJSON(*leasePath, &lease); err != nil {
			return err
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			receipt, err := orchestrator.RecordReceipt(corpusassurance.OrchestratorReceiptRequest{Lease: lease, BatchRoot: *batch}, time.Now().UTC())
			if err != nil {
				return err
			}
			if err := writeOrchestratorJSON(*output, receipt); err != nil {
				return err
			}
			return writeOrchestratorOutput(w, receipt)
		})
	case "cleanup-claim":
		flags := orchestratorFlags("cleanup-claim")
		database, campaign, worker := flags.String("db", "", ""), flags.String("campaign", "", ""), flags.String("worker", "", "")
		seconds, output := flags.Int("seconds", 0, ""), flags.String("output", "", "")
		if err := parseOrchestratorFlags(flags, args[1:]); err != nil {
			return err
		}
		if err := requiredAssuranceFlags(*database, *campaign, *worker, *output); err != nil || *seconds <= 0 {
			if err != nil {
				return err
			}
			return errors.New("positive cleanup claim seconds are required")
		}
		return withOrchestrator(*database, func(orchestrator *corpusassurance.Orchestrator) error {
			claim, err := orchestrator.ClaimCleanup(*campaign, *worker, time.Now().UTC(), time.Duration(*seconds)*time.Second)
			if err != nil {
				return err
			}
			if err := writeOrchestratorJSON(*output, claim); err != nil {
				return err
			}
			return writeOrchestratorOutput(w, claim)
		})
	default:
		return errors.New("unknown corpus assurance orchestrator operation")
	}
}

func validateOrchestratorWorkerExecutable(definition corpusassurance.OrchestratorCampaignDefinition, bundlePath, executingSHA string) error {
	if executingSHA == definition.Tools.SHA256 {
		return nil
	}
	if err := corpusassurance.ValidateOracleBundle(bundlePath); err != nil {
		return err
	}
	var bundle corpusassurance.OracleBundle
	if err := readOrchestratorJSON(bundlePath, &bundle); err != nil || !orchestratorWorkerExecutableMatches(definition, bundle, executingSHA, runtime.GOOS, runtime.GOARCH) {
		return errors.New("alternate worker does not match sealed bundle")
	}
	return nil
}

func orchestratorWorkerExecutableMatches(definition corpusassurance.OrchestratorCampaignDefinition, bundle corpusassurance.OracleBundle, executingSHA, goos, goarch string) bool {
	tools := definition.Tools
	if executingSHA == tools.SHA256 {
		return true
	}
	return bundle.Tools.Commit == tools.Commit && bundle.Tools.SHA256 == tools.SHA256 &&
		bundle.ToolsAMD64.Commit == tools.Commit && bundle.ToolsAMD64.OS == goos && bundle.ToolsAMD64.Arch == goarch &&
		bundle.ToolsAMD64.SHA256 == bundle.ToolsAMD64SHA256 && executingSHA == bundle.ToolsAMD64SHA256 &&
		definition.ControlledInputSHA256[corpusassurance.OrchestratorToolsAMD64Input] == executingSHA
}

func validateRawIngestRoot(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("raw-ingest root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("raw-ingest root must be a mode 0700 directory")
	}
	return nil
}

func writeOrchestratorSSHResult(w io.Writer, receipt corpusassurance.OrchestratorSSHDispatchReceipt, runErr error) error {
	if runErr != nil {
		if receipt.ActionRequired {
			if err := writeOrchestratorOutput(w, receipt); err != nil {
				return errors.Join(runErr, err)
			}
		}
		return runErr
	}
	return writeOrchestratorOutput(w, receipt)
}

func orchestratorFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet("corpus assurance orchestrator "+name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

func parseOrchestratorFlags(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	return nil
}

func withOrchestrator(path string, run func(*corpusassurance.Orchestrator) error) error {
	orchestrator, err := corpusassurance.OpenOrchestrator(path)
	if err != nil {
		return err
	}
	defer orchestrator.Close()
	return run(orchestrator)
}

func readOrchestratorJSON(path string, value any) error {
	_, err := readOrchestratorJSONBytes(path, value)
	return err
}

func readOrchestratorJSONBytes(path string, value any) ([]byte, error) {
	if path == "" {
		return nil, errors.New("orchestrator JSON path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if err := releasecontract.DecodeExactJSON(data, value); err != nil {
		return nil, err
	}
	return data, nil
}

func writeOrchestratorJSON(path string, value any) error {
	return corpusassurance.WriteNewJSON(path, value)
}

func writeOrchestratorOutput(w io.Writer, value any) error {
	return json.NewEncoder(w).Encode(value)
}
