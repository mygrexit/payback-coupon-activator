# payback-coupon-activator

A small self-hosted daemon that activates every pending coupon in your Payback account once or several times a day, and notifies you when it needs attention.

> **Unofficial project, not affiliated with PAYBACK GmbH.** It drives the payback.de website with a headless browser. Payback can change the site at any time and break this tool. Use it at your own risk and check Payback's terms of use.

> **Built with heavy use of agentic coding.** Most of this code was written by an AI coding agent, then reviewed and tested against the live payback.de site (October 2026, ~360 coupons activated). The cookie file gives full access to your account, so read the code before you run it.

## How it works

1. You log in on payback.de in your normal browser and export the cookies to `data/cookies.json`.
2. At every time listed in `RUN_AT` (default: once a day at 04:00), the daemon does the following:
   - loads those cookies into headless Chromium
   - opens `https://www.payback.de/coupons`
   - clicks every button in the "not activated" section, one at a time
   - reloads the page to verify
3. Payback may renew the session during a run. If so, the refreshed cookies are written back to the file.
4. You get a notification (Pushover, Telegram, ntfy, ...) in these cases:
   - **Session expired:** log in again and re-export the cookies
   - **Bot check shown:** the run is skipped
   - **Page layout changed:** the selectors need an update
   - **Coupons could not be activated:** for example, coupons that need a code

The tool never types your password and never solves or bypasses CAPTCHAs or bot checks. When a person is needed, it stops and tells you.

### Why not the private app API?

Older tools such as [theodm/python-coupons](https://github.com/theodm/python-coupons) used the app API at `services-ext.payback.de`. As of October 2026, its login endpoint (`/json/secureauthenticate`) sits behind an Imperva bot challenge that plain HTTP clients can't pass. That API also needs app credentials that are extracted from the Payback app.

## Setup

### Docker Compose

```sh
git clone https://github.com/mygrexit/payback-coupon-activator.git
cd payback-coupon-activator
cp .env.example .env
nano .env                     # notification URL, run time, PUID/PGID
mkdir -p data
# export cookies (see below) to data/cookies.json
chmod 600 data/cookies.json
docker compose up -d --build
```

The image is built for amd64 and arm64 (e.g. Raspberry Pi). It includes Chromium, so expect roughly 300–400 MB. The Docker setup has not been tested on real hardware yet. If something breaks, please open an issue.

Instead of building locally you can use the prebuilt image `ghcr.io/mygrexit/payback-coupon-activator:latest`.

### Unraid

The app is available in **Community Applications**: search for *payback-coupon-activator*. Put the exported `cookies.json` into the data folder (default `/mnt/user/appdata/payback-coupon-activator`). The container runs as `nobody:users` (99:100), so the file has to be readable and writable by that user.

### Exporting the cookies

1. In your desktop browser, log in at <https://www.payback.de> and open the **Coupons** page once.
2. Install the **Cookie-Editor** extension (Chrome/Firefox). With payback.de open, click *Export → JSON*.
   - A Netscape `cookies.txt` file also works, for example from "Get cookies.txt LOCALLY".
3. Save the export as `data/cookies.json` on the server. Paste the clipboard into a text editor. Don't open a `.json` file in Firefox and copy from its JSON viewer: that copies a formatted view, not JSON.
4. Optional: set `BROWSER_USER_AGENT` in `.env` to your browser's User-Agent. Some sessions only work from the browser they were created in. In Firefox, open `about:support` and look for "User Agent". In Chrome, open `chrome://version`.

The cookie file gives full access to your Payback account. Treat it like a password: never commit it, keep it `chmod 600`, and delete old copies.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `COOKIES_FILE` | `/data/cookies.json` | Exported cookies (Cookie-Editor JSON or Netscape format). It is rewritten with refreshed cookies after successful runs. |
| `NOTIFY_URL` | *(empty)* | A [shoutrrr](https://github.com/nicholas-fedor/shoutrrr) URL. Empty means no notifications. |
| `NOTIFY_ON_SUCCESS` | `false` | Also notify after successful runs. |
| `TZ` | `Europe/Berlin` | Time zone for `RUN_AT`. |
| `RUN_AT` | `04:00` | Daily run time(s) as `HH:MM` (24h). Separate several times with commas, e.g. `04:00,12:00,19:00`. DST-aware. |
| `RUN_ON_START` | `true` | Run once right after the container starts. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `BROWSER_USER_AGENT` | *(Chromium default)* | User-Agent to send. Set it to the one of the browser the cookies came from. |
| `CHROME_PATH` | auto | Path to Chromium/Chrome. Set in the image. |
| `CHROME_NO_SANDBOX` | `false` (`true` in the image) | Disables the Chromium sandbox, which can't run in a locked-down container. |
| `PUID` / `PGID` | `1000` | Host user/group that owns `./data` (compose only). |

Example `NOTIFY_URL` values:

```
pushover://shoutrrr:APP_TOKEN@USER_KEY
telegram://BOT_TOKEN@telegram?chats=CHAT_ID
ntfy://ntfy.sh/YOUR_TOPIC
```

Credentials, cookie values and the notify URL are never logged.

**Several runs per day:** Payback sometimes adds coupons during the day, so a second or third run can catch them earlier. Each run opens payback.de once, so keep the list short (two or three times a day is plenty). More runs mean more traffic that could trigger a bot check.

## Usage

```sh
docker compose logs -f                                   # follow logs
docker compose run --rm payback-coupons --once           # run one cycle now
docker compose run --rm payback-coupons --test-notify    # send a test notification
docker compose run --rm payback-coupons --version
```

Example log:

```
level=INFO msg="Starting Payback coupon activation"
level=INFO msg="Found 6 pending coupons"
level=INFO msg="Activated coupon: 10 FACH °P auf Artikel der Kategorie Autoteile & Zubehör!*"
level=INFO msg="Activated coupon: 22 FACH °P auf diadent!*"
...
level=INFO msg="Activation finished: 6 activated, 0 failed, 0 still pending" duration=9s
level=INFO msg="Next run scheduled" at=2026-10-11T04:00:00+02:00
```

Without Docker (needs Go and Chrome/Chromium):

```sh
go build -o payback-coupons ./cmd/payback-coupons
COOKIES_FILE=./data/cookies.json ./payback-coupons --once
```

`--once` exits with `0` on success, `1` if anything needs attention and `2` on invalid configuration. That makes it usable from cron or systemd timers.

## Troubleshooting

| Message | What to do |
|---|---|
| **Session expired / cookies missing** | Log in on payback.de on your PC, export the cookies again and replace `data/cookies.json`. If this happens right after a fresh export, the session is probably tied to your PC. Set `BROWSER_USER_AGENT` first. If it still fails, the session is likely tied to your PC's IP address. |
| **Bot check** | Payback or its CDN asked for human verification. The run is skipped and retried the next day. If it keeps happening, export fresh cookies and set `BROWSER_USER_AGENT`. The tool will not try to get around it. |
| **Page not recognized** | Payback probably changed the website. Run `--once --debug-snapshot`, look at `data/debug.png`, and adjust the selectors in `internal/activator/selectors.go`. **Delete `debug.png` afterwards: it shows your account data.** |
| **Refreshed cookies could not be saved** / **permission denied** | `./data` or `cookies.json` isn't writable by the container user. Set `PUID`/`PGID` in `.env` to the owner of `./data` (`id -u`, `id -g`). |
| **Coupons not activated / still pending** | Some offers can't be activated with a click (e.g. they need a code). They are reported but don't stop the run. |

## Development

```sh
go test -race ./...   # browser tests run if Chrome/Chromium is installed, otherwise they are skipped
```

Layout:

| Path | Purpose |
|---|---|
| `cmd/payback-coupons` | CLI and wiring |
| `internal/activator` | Headless browser run. Every payback.de-specific selector is in `selectors.go`. |
| `internal/cookies` | Cookie import and export |
| `internal/notify` | shoutrrr wrapper with secret redaction |
| `internal/scheduler` | DST-safe scheduling at one or more times per day |
| `internal/config` | Environment configuration |

## Contributing

Pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md), and never post cookies, screenshots or logs from your account.

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## License

MIT – see [LICENSE](LICENSE).
