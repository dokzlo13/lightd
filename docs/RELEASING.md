# Releasing lightd

Run `scripts/check.sh`, the migration-script tests, `govulncheck`, and a local container smoke before tagging. Use synthetic fixtures only. Review the complete diff and ensure private configurations, `.trash`, credentials, coordinates and deployment resource IDs are absent from Git and build inputs.

Update CHANGELOG and migration docs. Commit the tested source and push main. After CI passes, create a new immutable `vX.Y.Z` tag at that checked commit and push it. Never move an existing published tag.

The tag workflow runs quality gates, builds/tests native binaries with SQLite, packages dependency notices, tests the container under a read-only root, scans fixable high/critical vulnerabilities, pushes the image with provenance/SBOM, and only then publishes the GitHub release. The image platform is Linux amd64; binary platforms are Linux amd64/arm64, macOS arm64 and Windows amd64.

Verify all jobs and assets, checksums, `--version` source revision and registry digest. Hand operations the release tag, source SHA, exact image digest, migration/rollback procedure, tested configuration/script and remaining limitations. Deployment uses the digest through the infrastructure owner's normal GitOps workflow. Physical lamp behavior still needs controlled rollout validation; simulated acknowledgments are not proof of physical convergence.
