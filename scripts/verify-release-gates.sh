#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]] || [[ ! "$1" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || [[ ! "$2" =~ ^[0-9A-Fa-f]{40}$ ]]; then
	echo "usage: $0 OWNER/REPOSITORY 40_HEX_SHA" >&2
	exit 2
fi

repository="$1"
sha="$(printf '%s' "$2" | tr '[:upper:]' '[:lower:]')"

authority() {
	local workflow="$1" event="$2" job_name="$3" runs_json run run_id jobs_json job
	runs_json="$(gh api --method GET \
		"/repos/$repository/actions/workflows/$workflow/runs" \
		-f "head_sha=$sha" \
		-f status=completed \
		-f per_page=100)"
	if ! run="$(jq -ce --arg sha "$sha" --arg path ".github/workflows/$workflow" --arg event "$event" '
		def nonnegative_integer: type == "number" and . >= 0 and . == floor;
		def positive_integer: type == "number" and . > 0 and . == floor;
		def https_url: type == "string" and startswith("https://");
		def iso8601: type == "string" and ((try fromdateiso8601 catch false) != false);
		if type != "object"
			or ((.total_count | nonnegative_integer) | not)
			or (.workflow_runs | type) != "array"
			or .total_count != (.workflow_runs | length)
			or (all(.workflow_runs[];
				type == "object"
				and (.id | positive_integer)
				and .path == $path
				and .head_sha == $sha
				and .status == "completed"
				and (.conclusion | type) == "string"
				and (.event | type) == "string"
				and (.created_at | iso8601)
				and (.html_url | https_url)
			) | not)
		then error("malformed workflow runs")
		else [.workflow_runs[] | select(.event == $event and .conclusion == "success")] as $matches
		| ($matches | sort_by(.created_at, .id) | last) // error("missing successful workflow run")
		end
	' <<<"$runs_json")"; then
		echo "no successful $event authority for $workflow at $repository@$sha" >&2
		return 1
	fi

	run_id="$(jq -r '.id' <<<"$run")"
	jobs_json="$(gh api --method GET "/repos/$repository/actions/runs/$run_id/jobs" -f per_page=100)"
	if ! job="$(jq -ce --arg sha "$sha" --arg name "$job_name" '
		def nonnegative_integer: type == "number" and . >= 0 and . == floor;
		def positive_integer: type == "number" and . > 0 and . == floor;
		def https_url: type == "string" and startswith("https://");
		if type != "object"
			or ((.total_count | nonnegative_integer) | not)
			or (.jobs | type) != "array"
			or .total_count != (.jobs | length)
			or (all(.jobs[];
				type == "object"
				and (.id | positive_integer)
				and (.name | type) == "string"
				and .head_sha == $sha
				and (.status | type) == "string"
				and (.conclusion | type) == "string"
				and (.html_url | https_url)
			) | not)
		then error("malformed jobs")
		else [.jobs[] | select(.name == $name)] as $matches
		| if ($matches | length) == 1
			and $matches[0].status == "completed"
			and $matches[0].conclusion == "success"
		then $matches[0] else error("non-unique successful job") end
		end
	' <<<"$jobs_json")"; then
		echo "no unique successful $job_name job for $workflow at $repository@$sha" >&2
		return 1
	fi

	jq -cn --argjson run "$run" --argjson job "$job" '{
		event: $run.event,
		run_id: $run.id,
		run_url: $run.html_url,
		job_id: $job.id,
		job_url: $job.html_url
	}'
}

ci="$(authority ci.yml push test)"
full_fixtures="$(authority full-fixtures.yml workflow_dispatch full-fixtures)"

jq -n \
	--arg repository "$repository" \
	--arg sha "$sha" \
	--argjson ci "$ci" \
	--argjson full_fixtures "$full_fixtures" \
	'{
		schema_version: 1,
		repository: $repository,
		sha: $sha,
		conclusion: "success",
		ci: $ci,
		full_fixtures: $full_fixtures
	}'
