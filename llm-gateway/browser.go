package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"
)

type BrowserSession struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func NewBrowserSession() (*BrowserSession, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1280, 900),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	if err := chromedp.Do(ctx, chromedp.Navigate("about:blank")); err != nil {
		cancel()
		allocCancel()
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	return &BrowserSession{
		ctx: ctx,
		cancel: func() {
			cancel()
			allocCancel()
		},
	}, nil
}

func (b *BrowserSession) Close() {
	b.cancel()
}

func (b *BrowserSession) Navigate(url string) error {
	ctx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()
	return chromedp.Do(ctx, chromedp.Navigate(url))
}

func (b *BrowserSession) Click(selector string) error {
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()
	return chromedp.Do(ctx,
		chromedp.WaitVisible(chromedp.CSS(selector)),
		chromedp.Click(chromedp.CSS(selector)),
	)
}

func (b *BrowserSession) Type(selector, text string) error {
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()
	return chromedp.Do(ctx,
		chromedp.WaitVisible(chromedp.CSS(selector)),
		chromedp.Clear(chromedp.CSS(selector)),
		chromedp.SendKeys(chromedp.CSS(selector), text),
	)
}

func (b *BrowserSession) SelectOption(selector, value string) error {
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()
	return chromedp.Do(ctx,
		chromedp.SetValue(chromedp.CSS(selector), value),
	)
}

func (b *BrowserSession) Scroll(direction string) error {
	dy := 300
	if direction == "up" {
		dy = -300
	}
	_, err := chromedp.Run(b.ctx,
		chromedp.Evaluate[chromedp.Void](fmt.Sprintf(`window.scrollBy(0, %d)`, dy)),
	)
	return err
}

func (b *BrowserSession) Screenshot(savePath string) error {
	buf, err := chromedp.Run(b.ctx, chromedp.FullScreenshot(80))
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(savePath), 0755)
	return os.WriteFile(savePath, buf, 0644)
}

func (b *BrowserSession) CurrentURL() string {
	url, _ := chromedp.Run(b.ctx, chromedp.Location())
	return url
}

func (b *BrowserSession) ExtractDOMViaJS() ([]DOMElement, error) {
	const script = `
	(function() {
		var selectors = 'a, button, input, select, textarea, [role="button"], [onclick], [href]';
		var nodes = document.querySelectorAll(selectors);
		var results = [];
		var seen = {};
		for (var i = 0; i < nodes.length && results.length < 80; i++) {
			var el = nodes[i];
			var sel = '';
			if (el.id) {
				sel = '#' + el.id;
			} else if (el.name) {
				sel = el.tagName.toLowerCase() + '[name="' + el.name.replace(/"/g, '\\"') + '"]';
			} else {
				var tag = el.tagName.toLowerCase();
				// For inputs, use type attribute to distinguish
				if (tag === 'input' && el.type) {
					var typeSel = tag + '[type="' + el.type + '"]';
					if (document.querySelectorAll(typeSel).length === 1) {
						sel = typeSel;
					} else if (el.placeholder) {
						sel = tag + '[placeholder="' + el.placeholder.replace(/"/g, '\\"') + '"]';
					}
				}
				if (!sel) {
					var parent = el.parentElement;
					if (parent) {
						var siblings = parent.querySelectorAll(':scope > ' + tag);
						var ps = parent.id ? '#' + parent.id : '';
						if (!ps && parent.className) {
							var cls = parent.className.trim().split(/\s+/)[0];
							if (cls && document.querySelectorAll('.' + CSS.escape(cls)).length <= 3) {
								ps = '.' + CSS.escape(cls);
							}
						}
						if (!ps) ps = parent.tagName.toLowerCase();
						if (siblings.length === 1) {
							sel = ps + ' > ' + tag;
						} else {
							var idx = Array.prototype.indexOf.call(parent.children, el) + 1;
							sel = ps + ' > :nth-child(' + idx + ')';
						}
					} else {
						sel = tag;
					}
				}
			}
			// Verify selector uniqueness
			try {
				var matches = document.querySelectorAll(sel);
				if (matches.length > 1) {
					// Fall back to nth-of-type from body
					var all = document.querySelectorAll(el.tagName.toLowerCase());
					var globalIdx = Array.prototype.indexOf.call(all, el);
					sel = el.tagName.toLowerCase() + ':nth-of-type(' + (globalIdx + 1) + ')';
				}
			} catch(e) {}
			if (seen[sel]) continue;
			seen[sel] = true;
			results.push({
				tag: el.tagName.toLowerCase(),
				id: el.id || '',
				text: (el.textContent || '').trim().substring(0, 100),
				type: el.type || '',
				href: el.href || '',
				placeholder: el.placeholder || '',
				selector: sel
			});
		}
		return results;
	})()
	`
	result, err := chromedp.Run(b.ctx,
		chromedp.Evaluate[[]byte](script),
	)
	if err != nil {
		return nil, err
	}
	var elements []DOMElement
	if err := json.Unmarshal(result, &elements); err != nil {
		return nil, err
	}
	return elements, nil
}

func (b *BrowserSession) WaitLoad() error {
	return chromedp.Do(b.ctx, chromedp.Sleep(2*time.Second))
}
