package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Browser is the stable seam implemented by each native Rush adapter.
type Browser interface {
	LoadHTML(context.Context, string) error
	Evaluate(context.Context, string) (json.RawMessage, error)
}

type Conformance struct {
	DOM         bool `json:"dom"`
	Selection   bool `json:"selection"`
	BeforeInput bool `json:"beforeInput"`
	ShadowDOM   bool `json:"shadowDOM"`
	Iframe      bool `json:"iframe"`
}

func (c Conformance) Passed() bool {
	return c.DOM && c.Selection && c.BeforeInput && c.ShadowDOM && c.Iframe
}

type Sample struct {
	Page    time.Duration `json:"page"`
	Adapter time.Duration `json:"adapter"`
	Total   time.Duration `json:"total"`
}

type Measurement struct {
	Name    string        `json:"name"`
	Repeats int           `json:"repeats"`
	Median  Sample        `json:"median"`
	Min     time.Duration `json:"min"`
	Max     time.Duration `json:"max"`
	Target  time.Duration `json:"target"`
	Passed  bool          `json:"passed"`
}

type Performance struct {
	Assertions Measurement `json:"assertions"`
	DOM        Measurement `json:"dom"`
}

// RunConformance checks browser behavior at public web-platform seams. It does
// not use adapter internals and is shared by every native browser host.
func RunConformance(ctx context.Context, browser Browser) (Conformance, error) {
	if err := browser.LoadHTML(ctx, conformanceDocument); err != nil {
		return Conformance{}, err
	}
	raw, err := browser.Evaluate(ctx, conformanceScript)
	if err != nil {
		return Conformance{}, err
	}
	var result Conformance
	if err := json.Unmarshal(raw, &result); err != nil {
		return Conformance{}, fmt.Errorf("decode conformance result: %w", err)
	}
	if !result.Passed() {
		return result, fmt.Errorf("browser conformance failed: %+v", result)
	}
	return result, nil
}

// RunPerformance repeats warm in-page workloads and reports browser execution
// separately from native adapter overhead. Targets are the Rush product contract.
func RunPerformance(ctx context.Context, browser Browser, repeats int) (Performance, error) {
	if repeats < 1 {
		return Performance{}, fmt.Errorf("repeats must be positive")
	}
	if err := browser.LoadHTML(ctx, `<!doctype html><meta charset="utf-8"><body></body>`); err != nil {
		return Performance{}, err
	}
	assertions, err := measure(ctx, browser, "1,000 trivial assertions", repeats, 250*time.Millisecond, assertionScript)
	if err != nil {
		return Performance{}, err
	}
	dom, err := measure(ctx, browser, "1,000 DOM create/query/mutate operations", repeats, time.Second, domScript)
	if err != nil {
		return Performance{}, err
	}
	return Performance{Assertions: assertions, DOM: dom}, nil
}

func measure(ctx context.Context, browser Browser, name string, repeats int, target time.Duration, script string) (Measurement, error) {
	samples := make([]Sample, 0, repeats)
	for range repeats {
		started := time.Now()
		raw, err := browser.Evaluate(ctx, script)
		total := time.Since(started)
		if err != nil {
			return Measurement{}, err
		}
		var response struct {
			Milliseconds float64 `json:"milliseconds"`
			Passed       bool    `json:"passed"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return Measurement{}, err
		}
		if !response.Passed {
			return Measurement{}, fmt.Errorf("%s workload returned a failed result", name)
		}
		page := time.Duration(response.Milliseconds * float64(time.Millisecond))
		adapter := total - page
		if adapter < 0 {
			adapter = 0
		}
		samples = append(samples, Sample{Page: page, Adapter: adapter, Total: total})
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].Total < samples[j].Total })
	median := samples[len(samples)/2]
	return Measurement{Name: name, Repeats: repeats, Median: median, Min: samples[0].Total, Max: samples[len(samples)-1].Total, Target: target, Passed: median.Total < target}, nil
}

const conformanceDocument = `<!doctype html>
<meta charset="utf-8">
<body><div id="editor" contenteditable>Rush selection</div><div id="shadow"></div><iframe id="frame" srcdoc="<!doctype html><p id='inside'>iframe</p>"></iframe></body>`

const conformanceScript = `(async () => {
  const editor = document.querySelector('#editor');
  let beforeInput = false;
  editor.addEventListener('beforeinput', () => { beforeInput = true });
  editor.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, inputType: 'insertText', data: 'x' }));
  const range = document.createRange();
  range.selectNodeContents(editor);
  const selection = getSelection();
  selection.removeAllRanges();
  selection.addRange(range);
  const shadow = document.querySelector('#shadow').attachShadow({mode: 'open'});
  shadow.innerHTML = '<button>shadow button</button>';
  const frame = document.querySelector('#frame');
  if (!frame.contentDocument || !frame.contentDocument.querySelector('#inside')) {
    await new Promise(resolve => frame.addEventListener('load', resolve, {once: true}));
  }
  return {
    dom: editor instanceof HTMLDivElement,
    selection: selection.toString() === 'Rush selection',
    beforeInput,
    shadowDOM: shadow.querySelector('button').textContent === 'shadow button',
    iframe: frame.contentDocument.querySelector('#inside').textContent === 'iframe'
  };
})()`

const assertionScript = `(() => {
  const start = performance.now();
  let passed = true;
  for (let i = 0; i < 1000; i++) passed = passed && (i + 1 > i);
  return {passed, milliseconds: performance.now() - start};
})()`

const domScript = `(() => {
  const start = performance.now();
  const root = document.createElement('main');
  document.body.replaceChildren(root);
  let passed = true;
  for (let i = 0; i < 1000; i++) {
    const node = document.createElement('span');
    node.dataset.index = String(i);
    root.append(node);
    passed = passed && root.querySelector('[data-index="' + i + '"]') === node;
    node.textContent = 'updated';
  }
  return {passed, milliseconds: performance.now() - start};
})()`
