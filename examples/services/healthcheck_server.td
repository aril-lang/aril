// Showcase: the net/http binding and Go interface conformance.
// HealthHandler explicitly implements Go's http.Handler -- the conformance is
// declared and checked by Tide (decision D14), with no hand-written glue.
// Target sketch — syntax is illustrative (see docs/language-spec.md).
import http
import log

class HealthHandler implements http.Handler {
  serveHTTP(w: http.ResponseWriter, r: http.Request) {
    w.writeString("ok\n")
  }
}

func main() {
  let handler = HealthHandler{}
  log.println("listening on :8080")
  match http.listenAndServe(":8080", handler) {
    Ok(_)  => {},
    Err(e) => log.fatal(e),
  }
}
