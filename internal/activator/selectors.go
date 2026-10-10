package activator

// Everything that depends on payback.de's HTML lives in this file. When
// Payback changes its site, this is the place to update.
//
// Verified against the live site on 2026-10-10 (MUI/React page):
//
//	div[data-testid="not-activated-coupons-section"]
//	  div[data-testid="not-activated-coupon-item-<ID>"]
//	    div[data-testid="coupon-partner-logo-<ID>"] (img alt = partner)
//	    div[data-testid="coupon-headline-<ID>"]              "10FACH °P"
//	    p[data-testid="coupon-headline-description-<ID>"]    "auf den Einkauf!*"
//	    button[data-testid="coupon-button-<ID>-not_activated"] "Jetzt aktivieren"
//
// <ID> is numeric or partner-prefixed (e.g. "dm-med_123"). Coupons that
// need a code ("Zur Code-Eingabe") have no "-not_activated" button and
// are therefore not counted as pending. An activated coupon disappears
// from the not-activated section.
//
// The logged-in header shows an "Abmelden" (log out) link. A coupon page
// without it is treated as logged out, so an expired session can never be
// mistaken for "nothing pending". The exact logged-out page is not
// verified; it is also detected via a login URL, a visible password field
// or a login iframe.

// helpersJS defines functions shared by the snippets below.
const helpersJS = `
  const SECTION = '[data-testid="not-activated-coupons-section"]';
  const ITEM_PREFIX = "not-activated-coupon-item-";
  const BUTTON = 'button[data-testid^="coupon-button-"][data-testid$="-not_activated"]';
  const itemId = (el) => el.getAttribute("data-testid").slice(ITEM_PREFIX.length);
  // Pending coupons: items with an enabled activation button.
  const pendingItems = () => {
    const section = document.querySelector(SECTION);
    if (!section) return [];
    return Array.from(section.querySelectorAll('[data-testid^="' + ITEM_PREFIX + '"]')).filter((it) => {
      const b = it.querySelector(BUTTON);
      return b && !b.disabled;
    });
  };
  const textOf = (root, sel) => {
    const el = root.querySelector(sel);
    return el ? el.innerText.replace(/\s+/g, " ").trim() : "";
  };
  const labelOf = (item) => {
    const id = itemId(item);
    // The logo's alt text is usually the generic "Coupon Partner Logo".
    const img = item.querySelector('[data-testid="coupon-partner-logo-' + id + '"] img');
    const alt = img ? (img.getAttribute("alt") || "").trim() : "";
    const partner = /logo/i.test(alt) ? "" : alt;
    // The headline element also contains the description, so use the
    // label parts ("10" "FACH °P") for the headline itself.
    const parts = Array.from(item.querySelectorAll('[data-testid^="coupon-label-part-' + id + '-"]'))
      .map((e) => e.innerText.trim()).filter(Boolean);
    const head = parts.length ? parts.join(" ") : textOf(item, '[data-testid="coupon-headline-' + id + '"]');
    const desc = textOf(item, '[data-testid="coupon-headline-description-' + id + '"]');
    const text = [head, desc].filter(Boolean).join(" ");
    return ((partner ? partner + ": " : "") + text).slice(0, 120) || "coupon " + id;
  };
`

// inspectJS classifies the current page. It returns
// {state: "coupons"|"login"|"challenge"|"unknown", pending: <int>}.
//
// Order matters: challenge first (no coupons on a challenge page), then
// the pending section, then login markers.
const inspectJS = `(() => {` + helpersJS + `
  const q = (s) => document.querySelector(s);
  const text = (document.title + " " + (document.body ? document.body.innerText.slice(0, 2000) : "")).toLowerCase();
  const path = location.pathname.toLowerCase();

  const challenge =
    q('iframe[src*="challenges.cloudflare.com"]') || q('.cf-turnstile') || q('#challenge-form') ||
    q('#cf-challenge-running') || q('iframe[src*="_Incapsula_Resource"]') ||
    text.includes("just a moment") || text.includes("checking your browser") ||
    text.includes("incapsula incident");
  if (challenge) return {state: "challenge", pending: 0};

  const loggedIn = Array.from(document.querySelectorAll("a, button"))
    .some((e) => /abmelden|logout/i.test((e.innerText || "") + " " + (e.getAttribute("href") || "")));
  if (q(SECTION) && loggedIn) return {state: "coupons", pending: pendingItems().length};

  const loginPath = ["/login", "/anmelden", "/authenticate", "/signin"].some((p) => path.includes(p));
  const password = Array.from(document.querySelectorAll('input[type="password"]')).some((el) => el.offsetParent !== null);
  const loginFrame = q('iframe[src*="login"], iframe[src*="auth"]');
  if (loginPath || password || loginFrame || q(SECTION)) return {state: "login", pending: 0};

  return {state: "unknown", pending: 0};
})()`

// clickNextJS clicks the activation button of the first pending coupon
// whose ID is not in the given list and returns {id, label}, or
// {id: ""} when nothing is left. el.click() is used (like the public
// userscripts do) so overlays such as cookie banners do not intercept it.
const clickNextJS = `((tried) => {` + helpersJS + `
  const skip = new Set(tried);
  const item = pendingItems().find((it) => !skip.has(itemId(it)));
  if (!item) return {id: "", label: ""};
  const res = {id: itemId(item), label: labelOf(item)};
  item.querySelector(BUTTON).click();
  return res;
})`

// confirmJS reports the state of a clicked coupon: "done" when it left the
// not-activated section, "pending" while it is still there, "gone" when
// the coupon page itself is no longer shown (navigation) – which is not a
// confirmation.
const confirmJS = `((id) => {` + helpersJS + `
  const section = document.querySelector(SECTION);
  if (!section) return "gone";
  const item = section.querySelector('[data-testid="' + ITEM_PREFIX + CSS.escape(id) + '"]');
  return item && item.querySelector(BUTTON) ? "pending" : "done";
})`
