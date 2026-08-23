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
	"time"

	"github.com/glade-sh/glade/tools/internal/corpusassurance"
	"github.com/glade-sh/glade/tools/internal/releasecontract"
)

func runCorpusAssuranceOrchestrator(ctx context.Context, args []string, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 || isHelpArg(args[0]) {
		_, err := fmt.Fprintln(w, "glade-tools corpus assurance orchestrator <plan|init|enqueue|status|lease|heartbeat|reserve|receipt|worker-once|ssh-dispatch|worker-transfer|cleanup-takeover|cleanup-claim>")
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
		if err != nil || executingSHA != plan.Definition.Tools.SHA256 {
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
