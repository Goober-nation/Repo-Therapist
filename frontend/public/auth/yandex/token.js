// Helper page for the Yandex ID popup (redirect_uri). The SDK reads the token
// from location.hash and posts it to the page with the button, which must be
// this same origin. Never accept a target origin from the URL or use "*".
function showFailure() {
  window.history.replaceState(null, "", window.location.pathname)
  document.getElementById("status").textContent =
    "Не удалось завершить вход. Закройте это окно и повторите вход на сайте."
}

// Official usage: call on load, once the SDK script has finished loading.
// YaSendSuggestToken returns nothing and may still be reading location.hash,
// so the URL is left intact on success; the SDK closes this popup itself.
window.addEventListener("load", () => {
  try {
    if (typeof window.YaSendSuggestToken !== "function")
      throw new Error("SDK unavailable")
    window.YaSendSuggestToken(window.location.origin, {})
  } catch {
    showFailure()
  }
})
