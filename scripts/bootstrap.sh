#!/usr/bin/env bash
# One-time setup (rerun it to apply changes) of everything GitHub Actions
# needs to deploy:
#   - AWS: Terraform state bucket, GitHub OIDC provider, least-privilege deploy
#     role, the Lambda permissions boundary, and Lambda code signing (build
#     bucket, signing profile, code signing config) (infra/init)
#   - GitHub: a deployment environment that only accepts one branch (main) or
#     release tags, optionally with required reviewers, plus its secrets and
#     variables
#
# One AWS account per environment.
#
# Requirements: AWS admin credentials for the target account (for example
# AWS_PROFILE), gh authenticated as a repo admin, terraform >= 1.11, python3, jq.
#
# Usage: AWS_PROFILE=<profile> scripts/bootstrap.sh <aws-region> [github-environment]
#        DEPLOY_BRANCH=<branch> ... the only branch allowed to deploy (default: main).
#        DEPLOY_TAGS=<pattern> ... deploy from tags matching it instead, e.g. 'v*.*.*'
#                                  (production deploys published releases).
#        REVIEWERS=<login>[,<login>] ... GitHub users who must approve each deploy
#                                  (when unset, the environment keeps its current ones).
#        AUTO_APPROVE=1 ... skips Terraform's interactive approval.
#        GITHUB_REPO=<owner>/<repo> ... overrides the repo detected by gh.
#
# Nothing environment-specific is written to the repository: names are
# computed at runtime and stored as GitHub environment secrets and variables.
set -euo pipefail

region=${1:?usage: AWS_PROFILE=<profile> $0 <aws-region> [github-environment]}
environment=${2:-production}
deploy_branch=${DEPLOY_BRANCH:-main}
if [[ -n "${DEPLOY_TAGS:-}" ]]; then
  policy_type=tag policy_name=$DEPLOY_TAGS
else
  policy_type=branch policy_name=$deploy_branch
fi

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
init_dir="$root/infra/init"

repo=${GITHUB_REPO:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}
# The exact "sub" claim prefix GitHub puts in this repo's OIDC tokens. It
# differs when immutable subjects (owner/repo IDs) are enabled, e.g. after
# a rename, so ask GitHub instead of assuming a format.
oidc_subject_prefix=$(gh api "repos/${repo}/actions/oidc/customization/sub" --jq .sub_claim_prefix)
account_id=$(aws sts get-caller-identity --query Account --output text)
state_bucket="incognitochat-tfstate-${account_id}-${region}"

echo "Bootstrapping ${repo} (environment: ${environment}, deploys from ${policy_type} ${policy_name}) in AWS ${region}"

tf() { terraform -chdir="$init_dir" "$@"; }
backend_args=(-backend-config="bucket=${state_bucket}" -backend-config="region=${region}")
tf_vars=(
  -var "aws_region=${region}"
  -var "state_bucket_name=${state_bucket}"
  -var "github_oidc_subject_prefix=${oidc_subject_prefix}"
  -var "github_environment=${environment}"
)
apply_args=(-input=false "${tf_vars[@]}")
[[ "${AUTO_APPROVE:-}" == "1" ]] && apply_args+=(-auto-approve)

# --- AWS (Terraform) ----------------------------------------------------------
if aws s3api head-bucket --bucket "$state_bucket" --region "$region" 2>/dev/null; then
  tf init -input=false -reconfigure "${backend_args[@]}"
  tf apply "${apply_args[@]}"
else
  # First run: the state bucket doesn't exist yet. Apply with local state,
  # then move that state into the bucket that was just created.
  echo "State bucket not found, so this is the first run: using local state until the bucket exists"
  override="$init_dir/backend_override.tf"
  trap 'rm -f "$override"' EXIT
  printf 'terraform {\n  backend "local" {}\n}\n' >"$override"
  tf init -input=false -reconfigure
  tf apply "${apply_args[@]}"
  rm -f "$override"
  tf init -input=false -migrate-state -force-copy "${backend_args[@]}"
  rm -f "$init_dir/terraform.tfstate" "$init_dir/terraform.tfstate.backup"
fi

deploy_role_arn=$(tf output -raw deploy_role_arn)
builds_bucket=$(tf output -raw builds_bucket)
signing_profile=$(tf output -raw signing_profile_name)
code_signing_config=$(tf output -raw code_signing_config_arn)

# --- GitHub -------------------------------------------------------------------
echo "Configuring GitHub environment '${environment}'"

# Required reviewers: as given, or the environment's current ones (the PUT
# below replaces them).
if [[ -n "${REVIEWERS:-}" ]]; then
  reviewers=$(for login in ${REVIEWERS//,/ }; do gh api "users/${login}" --jq '{type: "User", id: .id}'; done | jq -sc .)
else
  reviewers=$(gh api "repos/${repo}/environments/${environment}" \
    --jq '[.protection_rules[]? | select(.type == "required_reviewers") | .reviewers[] | {type, id: .reviewer.id}]' \
    2>/dev/null || echo '[]')
fi
jq -n --argjson reviewers "$reviewers" \
  '{deployment_branch_policy: {protected_branches: false, custom_branch_policies: true}, reviewers: $reviewers}' |
  gh api --method PUT "repos/${repo}/environments/${environment}" --input - >/dev/null
echo "Required reviewers: $(jq length <<<"$reviewers")"

# Exactly one deployment policy: the branch, or the release tag pattern.
found=0
while read -r id type name; do
  [[ -z "$id" ]] && continue
  if [[ "$type" == "$policy_type" && "$name" == "$policy_name" ]]; then
    found=1
  else
    gh api --method DELETE "repos/${repo}/environments/${environment}/deployment-branch-policies/${id}" >/dev/null
    echo "Removed the deployment policy for ${type} ${name}"
  fi
done <<<"$(gh api "repos/${repo}/environments/${environment}/deployment-branch-policies" \
  --jq '.branch_policies[] | "\(.id) \(.type) \(.name)"')"
if [[ $found == 0 ]]; then
  gh api --method POST "repos/${repo}/environments/${environment}/deployment-branch-policies" \
    -f name="$policy_name" -f type="$policy_type" >/dev/null
fi

# Secrets (masked in public workflow logs); values are piped in so they don't
# appear in process arguments.
printf '%s' "$deploy_role_arn" | gh secret set AWS_ROLE_ARN --env "$environment" --repo "$repo"
printf '%s' "$account_id" | gh secret set AWS_ACCOUNT_ID --env "$environment" --repo "$repo"
printf '%s' "$state_bucket" | gh secret set TF_STATE_BUCKET --env "$environment" --repo "$repo"
printf '%s' "$builds_bucket" | gh secret set LAMBDA_BUILDS_BUCKET --env "$environment" --repo "$repo"
printf '%s' "$signing_profile" | gh secret set LAMBDA_SIGNING_PROFILE --env "$environment" --repo "$repo"
printf '%s' "$code_signing_config" | gh secret set LAMBDA_CODE_SIGNING_CONFIG --env "$environment" --repo "$repo"
gh variable set AWS_REGION --env "$environment" --repo "$repo" --body "$region"

if [[ $policy_type == tag ]]; then
  how_to_deploy="publish a release (merge the release pull request)"
else
  how_to_deploy="push to ${deploy_branch}"
fi
if ! gh secret list --env "$environment" --repo "$repo" --json name --jq '.[].name' | grep -qx BOT_TOKEN; then
  cat <<EOF

Done. One step left: store the Telegram bot token (from @BotFather) as an
environment secret. Run this yourself so the token never touches a file or
your shell history:

  gh secret set BOT_TOKEN --env ${environment} --repo ${repo}

Then deploy: ${how_to_deploy}.
EOF
else
  echo "Done. BOT_TOKEN is already set. To deploy: ${how_to_deploy}."
fi
