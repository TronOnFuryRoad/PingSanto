# Agent Release Pipeline

This describes how PingSanto agents are built, signed, published, and registered with the controller.

## Overview
- GitHub repository hosts the code (`/opt/pingsanto` layout).
- GitHub Actions workflow (`.github/workflows/agent-release.yml`) triggers on tags `v*`.
- Artifacts: single Linux x86_64 tarball plus manifest, SBOM, checksums, and detached signature.
- Public distribution via GitHub Releases.
- Controller upgrade plan automatically updated (optional) after release.
- Slack/email notifications optional via secrets.

## Prerequisites
1. **GitHub repo:** push the tree in `/opt/pingsanto` to a GitHub repository.
2. **Minisign keys:**
   ```bash
   minisign -G -p pingsanto-agent.pub -s pingsanto-agent.key
   ```
   - Store `pingsanto-agent.key` securely.
   - Add private key contents to GitHub Actions secret `MINISIGN_SECRET_KEY` (the base64 block between `BEGIN`/`END`).
   - Replace `agent/keys/pingsanto-agent.pub` with the generated public key so the agent embeds it at build time.
3. **Controller credentials:**
   - Controller URL (e.g., `https://controller.example.com`).
   - Admin bearer token (set via `ADMIN_BEARER_TOKEN` env on controller). Add to GitHub secret `CONTROLLER_ADMIN_TOKEN`.
   - GitHub secret `CONTROLLER_BASE_URL` with the base URL.
4. **Notifications:**
   - Slack webhook (optional) → secret `SLACK_WEBHOOK_URL`.
   - Email webhook/API endpoint (optional) → secret `EMAIL_WEBHOOK_URL`.
   - Controller toggle `notify_on_publish` must be enabled (default). Manage via `go run ./cmd/settingsctl --set true|false`.

## Workflow Steps
1. `git tag v1.2.3` and push (`git push origin v1.2.3`).
2. GitHub Actions job performs:
   - Checkout and Go build (Linux x86_64) into `build/pingsanto-agent`.
   - SBOM via Syft (`build/SBOM.json`).
   - Vulnerability scan via Grype (fails build on findings unless suppressed).
   - Create staging directory `build/pingsanto-agent_<version>_linux_x86_64/` with binary, manifest, README/LICENSE (if present).
   - Tarball creation `pingsanto-agent_<version>_linux_x86_64.tar.gz`.
   - SHA-256 checksum file `sha256.txt`.
   - Minisign signature `pingsanto-agent_<version>.sig`.
   - Upload all assets to GitHub Release (auto-created by workflow).
3. Post-release steps (if controller secrets set):
   - Determine channel: tags containing `canary` → `canary`, else `stable`.
   - POST plan to controller admin endpoint with artifact URLs and checksum.
4. Notifications:
   - Slack message with version reference if webhook configured **and** controller toggle permits.
   - Email webhook hit if configured and controller toggle permits.

## Manual Overrides
- To skip controller update, leave `CONTROLLER_ADMIN_TOKEN` unset.
- To rerun release (e.g., fix metadata), delete the GitHub release/tag and retag or manually trigger workflow.

## CLI Helpers
### `upgradectl`
`controller/cmd/upgradectl` can be built and used by release engineers to manually update plans:
```bash
cd controller
go run ./cmd/upgradectl \
  --base-url https://controller.example.com \
  --token $CONTROLLER_ADMIN_TOKEN \
  --channel stable \
  --version v1.2.3 \
  --artifact-url https://github.com/org/repo/releases/download/v1.2.3/pingsanto-agent_v1.2.3_linux_x86_64.tar.gz \
  --signature-url https://github.com/org/repo/releases/download/v1.2.3/pingsanto-agent_v1.2.3.sig \
  --sha256 <checksum>
```

This is useful if the automated step is disabled or for rollbacks.

### `settingsctl`
Use `controller/cmd/settingsctl` to read or update the notification toggle exposed by the controller:
```bash
cd controller
# Show current setting
go run ./cmd/settingsctl --base-url https://controller.example.com --token $CONTROLLER_ADMIN_TOKEN

# Disable notifications
go run ./cmd/settingsctl --base-url https://controller.example.com --token $CONTROLLER_ADMIN_TOKEN --set false
```

## Future Enhancements
- Switch signing to Sigstore Cosign/keyless once infrastructure is ready.
- Add provenance attestations (SLSA) to releases.
- Publish artifacts to additional mirrors (S3/CloudFront) for redundancy.
- Integrate approval gates (GitHub Environments) for canary → stable promotion.
- Extend controller to surface release notes/metadata in UI.
