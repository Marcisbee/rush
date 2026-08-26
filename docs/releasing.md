# Publishing Rush

Every push to `main` runs `.github/workflows/draft-release.yml`. The workflow creates or updates one semantic-version GitHub draft release, regenerates its release notes from merged pull requests, and replaces its executable archives for these supported targets:

- Linux amd64 and arm64.
- macOS amd64 and arm64.

The first automated release advances one patch beyond the version in `package.json`. Later drafts advance one patch beyond the newest published GitHub release. While a draft remains open, every merge keeps its version and refreshes its notes and binaries from the new `main` revision.

Each archive contains `bin/rush` and the matching browser API in `dist/`. Wait for the **Prepare draft release** workflow to succeed, review the generated notes, and publish the draft in GitHub. Those are the only routine release actions.

Publishing the draft runs `.github/workflows/publish.yml`. The release tag supplies the version for both `package.json` and `package-lock.json` in the publishing workspace, so no version commit is required. The workflow verifies that all four matching executable archives completed for the release's exact source commit before publishing `rush-webtest` with that exact version.

The npm workflow authenticates exclusively through trusted publishing and GitHub OIDC. It does not read an npm token or repository secret. Pull-request workflows have neither npm credentials nor permission to mint an npm publishing identity.

## First release

npm requires a package to exist before a trusted publisher can be attached. Publish `rush-webtest@0.1.0` once from a maintainer's local checkout using an interactive npm session with 2FA:

1. Merge the initial package work to `main`, then check out that exact commit locally.
2. Run the same build and validation used by the release workflow:
   ```bash
   npm ci --ignore-scripts
   npm run check
   npm run build
   go test ./...
   go build -o bin/rush ./cmd/rush
   npm pack --dry-run
   ```
3. Run `npm login`, confirm the expected account with `npm whoami`, and run `npm publish --access public`. Complete the interactive 2FA challenge when prompted. Do not create a GitHub release for `v0.1.0`; the first automated draft will be `v0.1.1`.
4. Confirm `npm view rush-webtest@0.1.0 version` returns `0.1.0`.
5. Create the `npm-publish` GitHub environment without a required-reviewer rule. Publishing the reviewed GitHub draft is the release approval.
6. Configure the package's npm trusted publisher with these exact values:
   - Provider: GitHub Actions
   - Organization or user: `Marcisbee`
   - Repository: `rush`
   - Workflow filename: `publish.yml`
   - Environment: `npm-publish`
   - Allowed action: `npm publish`
7. In the npm package publishing settings, require 2FA and disallow token publishing after the trusted publisher is active.

## Publishing verification

After a draft is published, npm 11 exchanges the workflow's GitHub OIDC identity for a short-lived publishing credential. The workflow builds and checks the package, previews the tarball, publishes it, waits for the exact release-tag version to appear on the public registry, installs that version into an empty consumer directory, and runs a Rush test against the installed package.
