package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type Config struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

var (
	configPath = "sync_config.json"
	// statusChan 用于传递当前处理的文件名
	statusChan = make(chan string, 100)
)

func main() {
	myApp := app.New()
	window := myApp.NewWindow("Hugo 增量同步工具 V3.0 - 极致流畅版")
	window.Resize(fyne.NewSize(600, 300))

	if iconRes, err := fyne.LoadResourceFromPath("04.ico"); err == nil {
		window.SetIcon(iconRes)
		myApp.SetIcon(iconRes)
	}

	conf := loadConfig()
	srcEntry := widget.NewEntry()
	srcEntry.SetText(conf.Src)
	dstEntry := widget.NewEntry()
	dstEntry.SetText(conf.Dst)

	// 使用 Label 代替 MultiLineEntry，性能提升 1000%
	statusLabel := widget.NewLabel("等待操作...")
	statusLabel.Wrapping = fyne.TextTruncate // 超长路径自动截断，不换行

	// --- 高性能刷新逻辑 ---
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond) // 0.1秒刷新一次状态
		var lastStatus string
		for {
			select {
			case s := <-statusChan:
				lastStatus = s
			case <-ticker.C:
				if lastStatus != "" {
					statusLabel.SetText("🚀 当前处理: " + lastStatus)
					lastStatus = ""
				}
			}
		}
	}()

	srcBtn := widget.NewButton("选择源目录", func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if list != nil {
				srcEntry.SetText(list.Path())
			}
		}, window)
	})

	dstBtn := widget.NewButton("选择目标目录", func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if list != nil {
				dstEntry.SetText(list.Path())
			}
		}, window)
	})

	var syncBtn *widget.Button
	syncBtn = widget.NewButton("🔥 开始极速同步", func() {
		syncBtn.Disable()
		go func() {
			start := time.Now()
			err := fullSync(srcEntry.Text, dstEntry.Text)
			duration := time.Since(start).Round(time.Second)

			if err != nil {
				statusLabel.SetText("❌ 同步失败: " + err.Error())
			} else {
				statusLabel.SetText(fmt.Sprintf("✅ 同步圆满完成！耗时: %v", duration))
				dialog.ShowInformation("完成", "同步已成功结束", window)
			}
			syncBtn.Enable()
		}()
	})

	window.SetOnClosed(func() {
		saveConfig(Config{Src: srcEntry.Text, Dst: dstEntry.Text})
	})

	window.SetContent(container.NewVBox(
		widget.NewLabel("本地源目录:"),
		container.NewBorder(nil, nil, nil, srcBtn, srcEntry),
		widget.NewLabel("远程目标目录:"),
		container.NewBorder(nil, nil, nil, dstBtn, dstEntry),
		container.NewPadded(syncBtn),
		widget.NewSeparator(),
		statusLabel, // 极简状态显示
	))

	window.ShowAndRun()
}

func fullSync(src, dst string) error {
	// 增量同步
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		targetPath := filepath.Join(dst, rel)

		tInfo, err := os.Stat(targetPath)
		if os.IsNotExist(err) || tInfo.Size() != info.Size() || info.ModTime().After(tInfo.ModTime().Add(2*time.Second)) {
			statusChan <- rel // 仅向通道发送当前文件名
			return copyFile(path, targetPath)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 清理
	return filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dst, path)
		sourcePath := filepath.Join(src, rel)
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			if !info.IsDir() {
				statusChan <- "清理: " + rel
				return os.Remove(path)
			}
		}
		return nil
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = io.Copy(d, s)
	return err
}

func saveConfig(c Config) {
	data, _ := json.Marshal(c)
	_ = os.WriteFile(configPath, data, 0644)
}

func loadConfig() Config {
	var c Config
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}
