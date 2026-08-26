# Publishing `rush-webtest`

All npm publishes run from `.github/workflows/publish.yml` after a GitHub release is published. Pull-request workflows have neither npm credentials nor permission to mint an npm publishing identity.

## First release

npm requires a package to exist before a trusted publisher can be attached. Bootstrap `rush-webtest@0.1.0` with a short-lived granular access token:

1. Create the `npm-publish` GitHub environment and require its normal deployment approval.
2. Create a granular npm token with read/write package permission, bypass 2FA, and the shortest practical expiration. Store it only as the `NPM_BOOTSTRAP_TOKEN` secret in the `npm-publish` environment.
3. Merge the release commit to `main`, create tag `v0.1.0` from that commit, and publish the matching GitHub release. The release workflow validates that the tag matches `package.json` and belongs to `main` before publishing.
4. After the workflow installs `rush-webtest@0.1.0` from the public registry and passes its real-WebKit smoke test, configure the package's npm trusted publisher with these exact values:
   - Provider: GitHub Actions
   - Organization or user: `Marcisbee`
   - Repository: `rush`
   - Workflow filename: `publish.yml`
   - Environment: `npm-publish`
   - Allowed action: `npm publish`
5. Delete the `NPM_BOOTSTRAP_TOKEN` environment secret and revoke the granular npm token.
6. In the npm package publishing settings, require 2FA and disallow token publishing after the trusted publisher is active.

The bootstrap token is only available to this release-only environment. It is never exposed to a pull-request job or stored in the repository.

## Later releases

Update `package.json` and `package-lock.json` to the same new version, merge that change to `main`, and publish a GitHub release whose tag is `v<version>`. npm 11 exchanges the workflow's GitHub OIDC identity for a short-lived publishing credential. No npm token or repository secret is used.

The workflow builds and checks the package, previews the tarball, publishes it, waits for the exact version to appear on the public registry, installs that version into an empty consumer directory, and runs a Rush test against the installed package.
