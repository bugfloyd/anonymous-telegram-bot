#!/usr/bin/env bash
# One-time setup (rerun it to apply changes) of everything GitHub Actions
# needs to deploy:
#   - AWS: Terraform state bucket, GitHub OIDC provider, least-privilege deploy
#     role and the Lambda permissions boundary (infra/init)
#   - GitHub: a deployment environment that only accepts the main branch, plus
#     its secrets and variables
#
# Requirements: AWS admin credentials for the target account (for example
# AWS_PROFILE), gh authenticated as a repo admin, terraform >= 1.11, python3.
#
# Usage: AWS_PROFILE=<profile> scripts/bootstrap.sh <aws-region> [github-environment]
#        AUTO_APPROVE=1 ... skips Terraform's interactive approval.
#        GITHUB_REPO=<owner>/<repo> ... overrides the repo detected by gh.
#
# Nothing environment-specific is written to the repository: names are
# computed at runtime and stored as GitHub environment secrets and variables.
set -euo pipefail

region=${1:?usage: AWS_PROFILE=<profile> $0 <aws-region> [github-environment]}
environment=${2:-production}

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
init_dir="$root/infra/init"

repo=${GITHUB_REPO:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}
account_id=$(aws sts get-caller-identity --query Account --output text)
state_bucket="incognito-chat-bot-tfstate-${account_id}-${region}"

echo "Bootstrapping ${repo} (environment: ${environment}) in AWS ${region}"

tf() { terraform -chdir="$init_dir" "$@"; }
backend_args=(-backend-config="bucket=${state_bucket}" -backend-config="region=${region}")
tf_vars=(
  -var "aws_region=${region}"
  -var "state_bucket_name=${state_bucket}"
  -var "github_repo=${repo}"
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

# --- GitHub -------------------------------------------------------------------
echo "Configuring GitHub environment '${environment}'"

# Only branches matching a deployment branch policy may deploy (main only).
gh api --method PUT "repos/${repo}/environments/${environment}" --input - >/dev/null <<'JSON'
{"deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}
JSON
if ! gh api "repos/${repo}/environments/${environment}/deployment-branch-policies" \
  --jq '.branch_policies[] | select(.type == "branch") | .name' | grep -qx main; then
  gh api --method POST "repos/${repo}/environments/${environment}/deployment-branch-policies" \
    -f name=main -f type=branch >/dev/null
fi

# Secrets (masked in public workflow logs); values are piped in so they don't
# appear in process arguments.
printf '%s' "$deploy_role_arn" | gh secret set AWS_ROLE_ARN --env "$environment" --repo "$repo"
printf '%s' "$account_id" | gh secret set AWS_ACCOUNT_ID --env "$environment" --repo "$repo"
printf '%s' "$state_bucket" | gh secret set TF_STATE_BUCKET --env "$environment" --repo "$repo"
gh variable set AWS_REGION --env "$environment" --repo "$repo" --body "$region"

if ! gh secret list --env "$environment" --repo "$repo" --json name --jq '.[].name' | grep -qx BOT_TOKEN; then
  cat <<EOF

Done. One step left: store the Telegram bot token (from @BotFather) as an
environment secret. Run this yourself so the token never touches a file or
your shell history:

  gh secret set BOT_TOKEN --env ${environment} --repo ${repo}

Then push to main (or run the CI/CD workflow manually on main) to deploy.
EOF
else
  echo "Done. BOT_TOKEN is already set; push to main to deploy."
fi
