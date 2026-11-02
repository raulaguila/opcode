//go:build desktop

package cmd

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// GTK, under the native window, must run on the process's main thread.
func init() { runtime.LockOSThread() }

const nativeWindowAvailable = true

// nativeWindow opens the app in a window of its own, through the system's
// web view, and returns when the window closes or ctx ends. The window loads
// the same local page as any other, with the run's key, so the page and its
// protections are the same in every window.
func nativeWindow(ctx context.Context, url string) error {
	page := fmt.Sprintf(`<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=%s"><body style="margin:0;background:#16161e">`, html.EscapeString(url))
	return wails.Run(&options.App{
		Title:            "opcode",
		Width:            1120,
		Height:           760,
		MinWidth:         680,
		MinHeight:        480,
		BackgroundColour: &options.RGBA{R: 22, G: 22, B: 30, A: 255},
		AssetServer: &assetserver.Options{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			fmt.Fprint(w, page)
		})},
		OnStartup: func(app context.Context) {
			// Ctrl+C in the terminal that started the app closes the window too.
			go func() {
				<-ctx.Done()
				wailsruntime.Quit(app)
			}()
		},
	})
}
