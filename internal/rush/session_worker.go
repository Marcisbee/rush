package rush

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/moxcomic/go-webview"
)

type workerPageEvent struct {
	ID     string          `json:"id,omitempty"`
	URL    string          `json:"url,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
	Timing Timing          `json:"timing"`
	Error  string          `json:"error,omitempty"`
}

// RunSessionWorker owns one WebView and serves coarse navigation/evaluation
// commands. A callback crosses the bridge once, then all of its DOM, storage,
// websocket, and application work executes locally in the client page.
func RunSessionWorker(headed bool, input io.Reader, output io.Writer) error {
	view, err := newWebView(headed)
	if err != nil {
		return fmt.Errorf("create session WebView: %w", err)
	}
	defer view.Destroy()

	ready := make(chan workerPageEvent, 8)
	results := make(chan workerPageEvent, 8)
	if err := view.Bind("__rushSessionReady", func(raw string) {
		var event workerPageEvent
		if json.Unmarshal([]byte(raw), &event) == nil {
			ready <- event
		}
	}); err != nil {
		return err
	}
	if err := view.Bind("__rushSessionResult", func(raw string) {
		var event workerPageEvent
		if json.Unmarshal([]byte(raw), &event) == nil {
			results <- event
		}
	}); err != nil {
		return err
	}
	view.Init(sessionWorkerBootstrap)
	view.SetTitle("Rush session client")
	view.SetSize(1280, 800, webview.HintNone)
	view.Navigate("about:blank")

	var writeMu sync.Mutex
	writeReply := func(reply sessionReply) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = json.NewEncoder(output).Encode(reply)
	}
	go func() {
		select {
		case <-ready:
		case <-time.After(15 * time.Second):
			view.Terminate()
			return
		}
		decoder := json.NewDecoder(input)
		for {
			var command sessionCommand
			if err := decoder.Decode(&command); err != nil {
				if !errors.Is(err, io.EOF) {
					writeReply(sessionReply{ID: command.ID, Error: err.Error()})
				}
				view.Terminate()
				return
			}
			if command.Action == "close" {
				view.Terminate()
				return
			}
			switch command.Action {
			case "goto":
				view.Dispatch(func() { view.Navigate(command.URL) })
				select {
				case event := <-ready:
					writeReply(sessionReply{ID: command.ID, URL: event.URL, Timing: event.Timing, Error: event.Error})
				case <-time.After(30 * time.Second):
					writeReply(sessionReply{ID: command.ID, Error: "session navigation timed out"})
				}
			case "evaluate", "reset":
				source := command.Source
				if command.Action == "reset" {
					source = `() => { try { localStorage.clear() } catch (_) {} try { sessionStorage.clear() } catch (_) {} try { document.cookie.split(";").forEach(value => { document.cookie = value.split("=")[0].trim() + "=;expires=Thu, 01 Jan 1970 00:00:00 GMT;path=/" }) } catch (_) {} document.body.replaceChildren() }`
				}
				idJSON, _ := json.Marshal(command.ID)
				sourceJSON, _ := json.Marshal(source)
				view.Dispatch(func() { view.Eval(fmt.Sprintf("window.__rushSessionWorker.evaluate(%s,%s)", idJSON, sourceJSON)) })
				select {
				case event := <-results:
					writeReply(sessionReply{ID: command.ID, URL: event.URL, Value: event.Value, Timing: event.Timing, Error: event.Error})
				case <-time.After(30 * time.Second):
					writeReply(sessionReply{ID: command.ID, Error: "session evaluation timed out"})
				}
			default:
				writeReply(sessionReply{ID: command.ID, Error: "unknown session worker action: " + command.Action})
			}
		}
	}()
	view.Run()
	return nil
}

const sessionWorkerBootstrap = `(() => {
  "use strict";
  if (window.__rushSessionWorker) return;
  const nativeSetTimeout = window.setTimeout.bind(window);
  const nativeSetInterval = window.setInterval.bind(window);
  let waitMs = 0;
  window.setTimeout = (callback, delay = 0, ...args) => {
    const requested = Math.max(0, Number(delay) || 0);
    return nativeSetTimeout((...values) => { waitMs += requested; callback(...values); }, requested, ...args);
  };
  window.setInterval = (callback, delay = 0, ...args) => {
    const requested = Math.max(0, Number(delay) || 0);
    return nativeSetInterval((...values) => { waitMs += requested; callback(...values); }, requested, ...args);
  };
  const formatError = error => {
    if (!error) return String(error);
    const message = (error.name || "Error") + (error.message ? ": " + error.message : "");
    return error.stack ? String(error.stack) : message;
  };
  async function evaluate(id, source) {
    waitMs = 0;
    performance.clearResourceTimings();
    const started = performance.now();
    try {
      const callback = (0, eval)("(" + source + ")\n//# sourceURL=rush-session-evaluate.js");
      if (typeof callback !== "function") throw new TypeError("session evaluate requires a callback");
      const value = await callback();
      const total = performance.now() - started;
      const network = performance.getEntriesByType("resource").reduce((sum, entry) => sum + entry.duration, 0);
      await window.__rushSessionResult(JSON.stringify({id, url: location.href, value, timing: {
        runner_ms: 0, application_ms: Math.max(0, total - network - waitMs), network_ms: network,
        wait_ms: waitMs, total_ms: total, build_ms: 0,
      }}));
    } catch (error) {
      const total = performance.now() - started;
      await window.__rushSessionResult(JSON.stringify({id, url: location.href, error: formatError(error), timing: {
        runner_ms: 0, application_ms: 0, network_ms: 0, wait_ms: waitMs, total_ms: total, build_ms: 0,
      }}));
    }
  }
  window.__rushSessionWorker = {evaluate};
  addEventListener("DOMContentLoaded", () => {
    const navigation = performance.getEntriesByType("navigation")[0];
    const total = navigation ? navigation.duration : 0;
    const network = navigation ? Math.max(0, navigation.responseEnd - navigation.fetchStart) : 0;
    window.__rushSessionReady(JSON.stringify({url: location.href, timing: {
      runner_ms: 0, application_ms: Math.max(0, total - network), network_ms: network,
      wait_ms: 0, total_ms: total, build_ms: 0,
    }}));
  }, {once: true});
})();`

func SessionWorkerMain(headed bool) error {
	return RunSessionWorker(headed, os.Stdin, os.Stdout)
}
