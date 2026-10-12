# Contributing

Thanks for helping out! Bug reports, fixes for changed payback.de selectors and small improvements are all welcome.

## Never share account data

The cookie file gives full access to a PAYBACK account. **Never** post any of the following in issues, pull requests or commits:

- `cookies.json` or parts of it
- `debug.png` screenshots
- logs or HTML that show your name, card number, points or coupons

If you need to show the page structure, cut it down to the relevant elements and remove personal data first.

## Workflow

1. Fork the repository and create a branch from `main`.
2. Make your change. Keep pull requests small and focused on one thing.
3. Run the checks locally:

   ```sh
   gofmt -l .            # must print nothing
   go vet ./...
   go test -race ./...   # browser tests run if Chrome/Chromium is installed
   ```

4. Add an entry under *Unreleased* in [CHANGELOG.md](CHANGELOG.md) if users will notice the change.
5. Open a pull request against `main`. CI runs the tests and a Docker build for amd64 and arm64. A PR can only be merged once CI passes.

## Where things are

- Every payback.de-specific selector is in `internal/activator/selectors.go`. If Payback changes the website, that is usually the only file that needs an update.
- Configuration is read in `internal/config`. New environment variables also need an entry in the README table and in `.env.example`.
- Scheduling lives in `internal/scheduler` and must stay DST-safe. Please add test cases for daylight saving changes when you touch it.

## Ground rules for the browser automation

This tool must never type passwords, solve CAPTCHAs or work around bot checks. Pull requests that add such behaviour will not be merged. When a person is needed, the tool stops and notifies the user.

## Releases

Maintainers merge into `main`, which publishes the `latest` image. Releases are tagged as `vX.Y.Z` and publish a matching image tag.
