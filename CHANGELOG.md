# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/).

## [0.2.0] - Unreleased

### Added

- `RUN_AT` accepts several times separated by commas, e.g. `04:00,12:00,19:00`. Each time is DST-aware. A single time such as `04:00` works as before.
- Unraid setup section in the README.
- This changelog.

### Changed

- Clearer `RUN_AT` error message for invalid values.

## [0.1.0] - 2026-10-10

### Added

- First release: activates all pending PAYBACK coupons once a day with an exported payback.de browser session in headless Chromium.
- Notifications through shoutrrr when the session expires, a bot check appears, the page layout changes or coupons can't be activated.
- Docker image for amd64 and arm64, published to `ghcr.io/mygrexit/payback-coupon-activator`.

[0.2.0]: https://github.com/mygrexit/payback-coupon-activator/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/mygrexit/payback-coupon-activator/releases/tag/v0.1.0
