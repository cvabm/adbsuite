package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "ADB Suite",
		Width:            1280,
		Height:           840,
		MinWidth:         960,
		MinHeight:        640,
		Frameless:        false,
		DisableResize:    false,
		CSSDragProperty:  "--wails-draggable",
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// 与前端 #f3f5f9 一致，减少窗口底色与页面切换闪一下
		BackgroundColour: &options.RGBA{R: 243, G: 245, B: 249, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Light,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
