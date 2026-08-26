# Publishing `rush-webtest`

Automated npm publishes run from `.github/workflows/publish.yml` after a GitHub release is published. The workflow authenticates exclusively through npm trusted publishing and GitHub OIDC. It does not read an npm token or repository secret. Pull-request workflows have neither npm credentials nor permission to mint an npm publishing identity.

## First release

npm requires a package to exist before a trusted publisher can be attached. Publish `rush-webtest@0.1.0` once from a maintainer's local checkout using an interactive npm session with 2FA:

1. Merge the release commit to `main`, then check out that exact commit locally.
2. Run the same build and validation used by the release workflow:
   ```bash
   npm ci --ignore-scripts
   npm run check
   npm run build
   go test ./...
   go build -o bin/rush ./cmd/rush
   npm pack --dry-run
   ```
3. Run `npm login`, confirm the expected account with `npm whoami`, and run `npm publish --access public`. Complete the interactive 2FA challenge when prompted. Do not create a GitHub release for `v0.1.0`, because the release workflow would attempt to publish the same immutable version again.
4. Confirm `npm view rush-webtest@0.1.0 version` returns `0.1.0`.
5. Create the `npm-publish` GitHub environment and require its normal deployment approval.
6. Configure the package's npm trusted publisher with these exact values:
   - Provider: GitHub Actions
   - Organization or user: `Marcisbee`
   - Repository: `rush`
   - Workflow filename: `publish.yml`
   - Environment: `npm-publish`
   - Allowed action: `npm publish`
7. In the npm package publishing settings, require 2FA and disallow token publishing after the trusted publisher is active.

## Later releases

Update `package.json` and `package-lock.json` to the same new version, merge that change to `main`, and publish a GitHub release whose tag is `v<version>`. npm 11 exchanges the workflow's GitHub OIDC identity for a short-lived publishing credential. No npm token or repository secret is used.

The workflow builds and checks the package, previews the tarball, publishes it, waits for the exact version to appear on the public registry, installs that version into an empty consumer directory, and runs a Rush test against the installed package.
