#!/usr/bin/env bash
set -euo pipefail

REPO="devararishivian/workstation-doctor"

echo "Setting up branch protection for ${REPO}..."

# Protect 'main' branch
echo "Configuring protection for 'main'..."
gh api -X PUT "repos/${REPO}/branches/main/protection" \
  -H "Accept: application/vnd.github+json" \
  --input - <<EOF
{
  "required_status_checks": null,
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "block_creations": false
}
EOF

# Protect 'develop' branch
echo "Configuring protection for 'develop'..."
gh api -X PUT "repos/${REPO}/branches/develop/protection" \
  -H "Accept: application/vnd.github+json" \
  --input - <<EOF
{
  "required_status_checks": null,
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "block_creations": false
}
EOF

echo "Branch protection successfully applied to 'main' and 'develop'!"
