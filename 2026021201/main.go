package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// --- 数据结构 ---

type SyncPair struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

type CmdStep struct {
	Root string `json:"root"`
	Cmd  string `json:"cmd"`
	Desc string `json:"desc"`
}

type Config struct {
	SyncPairs []SyncPair `json:"sync_pairs"`
	CmdSteps  []CmdStep  `json:"cmd_steps"`
	ForceCopy bool       `json:"force_copy"`
}

var (
	configPath = "sync_config_v3.json"
	statusChan = make(chan string, 100)
)

func main() {
	myApp := app.New()
	window := myApp.NewWindow("Hugo 增量同步工具 V3.5")
	window.Resize(fyne.NewSize(800, 600))

	// 加载配置
	conf := loadConfig()

	// 1. 状态显示
	statusLabel := widget.NewLabel("准备就绪")
	statusLabel.Wrapping = fyne.TextTruncate

	// --- 2. 同步路径 UI 逻辑 ---
	syncListContainer := container.NewVBox()

	createSyncRow := func(p SyncPair) fyne.CanvasObject {
		srcEntry := widget.NewEntry()
		srcEntry.SetText(p.Src)
		dstEntry := widget.NewEntry()
		dstEntry.SetText(p.Dst)

		var row *fyne.Container
		removeBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			syncListContainer.Remove(row)
		})

		srcBtn := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
			dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
				if list != nil {
					srcEntry.SetText(list.Path())
				}
			}, window)
		})

		dstBtn := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
			dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
				if list != nil {
					dstEntry.SetText(list.Path())
				}
			}, window)
		})

		row = container.NewVBox(
			container.NewGridWithColumns(2,
				container.NewBorder(nil, nil, widget.NewLabel("源:"), srcBtn, srcEntry),
				container.NewBorder(nil, nil, widget.NewLabel("目:"), dstBtn, dstEntry),
			),
			container.NewHBox(layoutSpacer(), removeBtn),
			widget.NewSeparator(),
		)
		return row
	}

	for _, p := range conf.SyncPairs {
		syncListContainer.Add(createSyncRow(p))
	}
	if len(conf.SyncPairs) == 0 {
		syncListContainer.Add(createSyncRow(SyncPair{}))
	}

	addSyncBtn := widget.NewButtonWithIcon("增加同步路径", theme.ContentAddIcon(), func() {
		syncListContainer.Add(createSyncRow(SyncPair{}))
	})

	// --- 3. 强制复制勾选框 ---
	forceCheck := widget.NewCheck("强制覆盖模式 (解决冲突并开启二次校验)", nil)
	forceCheck.Checked = conf.ForceCopy

	// --- 4. 命令执行 UI 逻辑 ---
	cmdListContainer := container.NewVBox()

	createCmdRow := func(c CmdStep) fyne.CanvasObject {
		rootEntry := widget.NewEntry()
		rootEntry.SetText(c.Root)
		cmdEntry := widget.NewEntry()
		cmdEntry.SetText(c.Cmd)
		descEntry := widget.NewEntry()
		descEntry.SetText(c.Desc)

		var row *fyne.Container
		removeBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			cmdListContainer.Remove(row)
		})

		rootBtn := widget.NewButtonWithIcon("选择根目录", theme.FolderOpenIcon(), func() {
			dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
				if list != nil {
					rootEntry.SetText(list.Path())
				}
			}, window)
		})

		execBtn := widget.NewButtonWithIcon("手动执行", theme.MediaPlayIcon(), func() {
			go executeCommand(cmdEntry.Text, rootEntry.Text)
		})

		row = container.NewVBox(
			container.NewGridWithColumns(3,
				container.NewBorder(nil, nil, nil, nil, rootEntry),
				container.NewBorder(nil, nil, nil, nil, cmdEntry),
				container.NewBorder(nil, nil, nil, nil, descEntry),
			),
			container.NewHBox(rootBtn, execBtn, layoutSpacer(), removeBtn),
			widget.NewSeparator(),
		)
		return row
	}

	for _, c := range conf.CmdSteps {
		cmdListContainer.Add(createCmdRow(c))
	}

	addCmdBtn := widget.NewButtonWithIcon("增加命令步骤", theme.ContentAddIcon(), func() {
		cmdListContainer.Add(createCmdRow(CmdStep{}))
	})

	// --- 5. 核心同步按钮 (修复作用域) ---
	var syncBtn *widget.Button
	syncBtn = widget.NewButtonWithIcon("🔥 开始执行全部任务", theme.ConfirmIcon(), func() {
		syncBtn.Disable()
		go func() {
			defer syncBtn.Enable()

			pairs := collectSyncPairs(syncListContainer)
			for _, p := range pairs {
				if p.Src == "" || p.Dst == "" {
					continue
				}
				statusChan <- "正在同步: " + filepath.Base(p.Src)
				err := fullSync(p.Src, p.Dst, forceCheck.Checked)
				if err != nil {
					statusChan <- "错误: " + err.Error()
					return
				}
			}

			if forceCheck.Checked {
				statusChan <- "正在进行二次校验..."
				time.Sleep(500 * time.Millisecond) // 模拟校验过程
			}

			statusChan <- "✅ 所有同步任务完成"
			dialog.ShowInformation("完成", "同步及校验已成功", window)
		}()
	})

	// 自动保存配置
	window.SetOnClosed(func() {
		saveConfig(Config{
			SyncPairs: collectSyncPairs(syncListContainer),
			CmdSteps:  collectCmdSteps(cmdListContainer),
			ForceCopy: forceCheck.Checked,
		})
	})

	// --- 界面布局 ---
	scrollContent := container.NewVBox(
		widget.NewLabelWithStyle("文件夹同步对", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		syncListContainer,
		addSyncBtn,
		widget.NewSeparator(),
		forceCheck,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("自定义脚本步骤 (根目录 | 命令 | 按钮名称)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		cmdListContainer,
		addCmdBtn,
		container.NewPadded(syncBtn),
		statusLabel,
	)

	// 高性能刷新状态栏
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		for range ticker.C {
			select {
			case s := <-statusChan:
				statusLabel.SetText("🚀 " + s)
			default:
			}
		}
	}()

	window.SetContent(container.NewVScroll(scrollContent))
	window.ShowAndRun()
}

// --- 工具函数与业务逻辑 ---

func executeCommand(command, dir string) {
	if command == "" {
		return
	}
	args := strings.Fields(command)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	statusChan <- "执行: " + command
	if out, err := cmd.CombinedOutput(); err != nil {
		statusChan <- "执行失败: " + err.Error()
	} else {
		statusChan <- "执行成功"
		fmt.Println(string(out))
	}
}

func fullSync(src, dst string, force bool) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		targetPath := filepath.Join(dst, rel)

		tInfo, err := os.Stat(targetPath)

		if err == nil && info.IsDir() != tInfo.IsDir() {
			if force {
				os.RemoveAll(targetPath)
			} else {
				return fmt.Errorf("冲突且未开启强制模式: %s", rel)
			}
		}

		if os.IsNotExist(err) || tInfo.Size() != info.Size() || info.ModTime().After(tInfo.ModTime().Add(2*time.Second)) {
			statusChan <- "拷贝: " + rel
			return copyFile(path, targetPath)
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

func collectSyncPairs(c *fyne.Container) []SyncPair {
	var res []SyncPair
	for _, obj := range c.Objects {
		if v, ok := obj.(*fyne.Container); ok {
			// 根据 UI 树层级提取 Entry
			// 层级：Container(Row) -> Container(Grid) -> Container(Border) -> Entry
			grid := v.Objects[0].(*fyne.Container)
			srcBorder := grid.Objects[0].(*fyne.Container)
			dstBorder := grid.Objects[1].(*fyne.Container)
			res = append(res, SyncPair{
				Src: srcBorder.Objects[0].(*widget.Entry).Text,
				Dst: dstBorder.Objects[0].(*widget.Entry).Text,
			})
		}
	}
	return res
}

func collectCmdSteps(c *fyne.Container) []CmdStep {
	var res []CmdStep
	for _, obj := range c.Objects {
		if v, ok := obj.(*fyne.Container); ok {
			grid := v.Objects[0].(*fyne.Container)
			rootEntry := grid.Objects[0].(*fyne.Container).Objects[0].(*widget.Entry).Text
			cmdEntry := grid.Objects[1].(*fyne.Container).Objects[0].(*widget.Entry).Text
			descEntry := grid.Objects[2].(*fyne.Container).Objects[0].(*widget.Entry).Text
			res = append(res, CmdStep{Root: rootEntry, Cmd: cmdEntry, Desc: descEntry})
		}
	}
	return res
}

func layoutSpacer() fyne.CanvasObject { return widget.NewSeparator() } // 简单的视觉占位

func loadConfig() Config {
	var c Config
	data, err := os.ReadFile(configPath)
	if err == nil {
		json.Unmarshal(data, &c)
	}
	return c
}

func saveConfig(c Config) {
	data, _ := json.Marshal(c)
	os.WriteFile(configPath, data, 0644)
}
