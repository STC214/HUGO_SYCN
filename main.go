package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// --- 數據結構 ---
type TaskType string

const (
	TaskSync TaskType = "SYNC"
	TaskCmd  TaskType = "CMD"
)

type TaskItem struct {
	Type    TaskType `json:"type"`
	GroupID int      `json:"group_id"`
	Src     string   `json:"src"`
	Dst     string   `json:"dst"`
	Root    string   `json:"root"`
	Cmd     string   `json:"cmd"`
	Desc    string   `json:"desc"`
}

type Config struct {
	Tasks      []TaskItem `json:"tasks"`
	GroupOrder string     `json:"group_order"`
	ForceCopy  bool       `json:"force_copy"`
}

var (
	configPath = "sync_config_v4.json"
	statusChan = make(chan string, 100)
)

func main() {
	myApp := app.New()
	window := myApp.NewWindow("Hugo 任務編組工具 V4.9 (UI 穩定版)")

	// 1. 鎖定窗口初始大小
	initialSize := fyne.NewSize(900, 750)
	window.Resize(initialSize)

	conf := loadConfig()

	// 2. 優化狀態欄：取消截斷，改為換行模式，保證文字完整
	statusLabel := widget.NewLabel("準備就緒")
	statusLabel.Alignment = fyne.TextAlignCenter
	statusLabel.Wrapping = fyne.TextWrapBreak // 自動換行，不再顯示 ...

	taskListContainer := container.NewVBox()

	// --- 任務行創建函數 ---
	var createSyncRow func(TaskItem) fyne.CanvasObject
	createSyncRow = func(t TaskItem) fyne.CanvasObject {
		groupEntry := widget.NewEntry()
		groupEntry.SetText(fmt.Sprintf("%d", t.GroupID))
		srcEntry := widget.NewEntry()
		srcEntry.SetText(t.Src)
		dstEntry := widget.NewEntry()
		dstEntry.SetText(t.Dst)
		var wrapper *fyne.Container
		removeBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			taskListContainer.Remove(wrapper)
			taskListContainer.Refresh()
		})
		innerRow := container.NewVBox(
			container.NewHBox(widget.NewLabel("分組ID:"), groupEntry, widget.NewLabel("【同步任務】")),
			container.NewGridWithColumns(2,
				container.NewBorder(nil, nil, nil, widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
					dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
						if list != nil {
							srcEntry.SetText(list.Path())
						}
					}, window)
				}), srcEntry),
				container.NewBorder(nil, nil, nil, widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
					dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
						if list != nil {
							dstEntry.SetText(list.Path())
						}
					}, window)
				}), dstEntry),
			),
			container.NewHBox(widget.NewSeparator(), removeBtn),
		)
		wrapper = container.NewPadded(innerRow)
		return wrapper
	}

	var createCmdRow func(TaskItem) fyne.CanvasObject
	createCmdRow = func(t TaskItem) fyne.CanvasObject {
		groupEntry := widget.NewEntry()
		groupEntry.SetText(fmt.Sprintf("%d", t.GroupID))
		rootEntry := widget.NewEntry()
		rootEntry.SetText(t.Root)
		cmdEntry := widget.NewEntry()
		cmdEntry.SetText(t.Cmd)
		descEntry := widget.NewEntry()
		descEntry.SetText(t.Desc)
		var wrapper *fyne.Container
		removeBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			taskListContainer.Remove(wrapper)
			taskListContainer.Refresh()
		})
		innerRow := container.NewVBox(
			container.NewHBox(widget.NewLabel("分組ID:"), groupEntry, widget.NewLabel("【腳本命令】")),
			container.NewGridWithColumns(3, rootEntry, cmdEntry, descEntry),
			container.NewHBox(widget.NewLabel("根目錄 / 執行命令 / 按鈕名"), removeBtn),
		)
		wrapper = container.NewPadded(innerRow)
		return wrapper
	}

	for _, t := range conf.Tasks {
		if t.Type == TaskSync {
			taskListContainer.Add(createSyncRow(t))
		} else {
			taskListContainer.Add(createCmdRow(t))
		}
	}

	// --- 底部控制區 ---
	orderEntry := widget.NewEntry()
	orderEntry.SetText(conf.GroupOrder)
	forceCheck := widget.NewCheck("強制覆蓋模式", nil)
	forceCheck.Checked = conf.ForceCopy

	var syncBtn *widget.Button
	syncBtn = widget.NewButtonWithIcon("🔥 開始按順序執行", theme.MediaPlayIcon(), func() {
		syncBtn.Disable()
		go func() {
			defer syncBtn.Enable()

			// 記錄當前尺寸
			currentSize := window.Canvas().Size()

			tasks := collectAllTasks(taskListContainer)
			orders := strings.Split(orderEntry.Text, ",")

			for _, gID := range orders {
				gID = strings.TrimSpace(gID)
				if gID == "" {
					continue
				}
				statusChan <- "正在運行組: " + gID
				for _, t := range tasks {
					if fmt.Sprintf("%d", t.GroupID) == gID {
						if t.Type == TaskSync {
							fullSync(t.Src, t.Dst, forceCheck.Checked)
						} else {
							executeCommand(t.Cmd, t.Root)
						}
					}
				}
			}
			statusChan <- "✅ 全部組任務執行完畢"

			// 3. 精準刷新並鎖死尺寸
			time.Sleep(200 * time.Millisecond)
			window.Content().Refresh()
			window.Resize(currentSize)
		}()
	})

	addBtnsRow := container.NewHBox(
		widget.NewButtonWithIcon("加同步對", theme.ContentAddIcon(), func() {
			taskListContainer.Add(createSyncRow(TaskItem{Type: TaskSync, GroupID: 1}))
			taskListContainer.Refresh()
		}),
		widget.NewButtonWithIcon("加命令行", theme.ContentAddIcon(), func() {
			taskListContainer.Add(createCmdRow(TaskItem{Type: TaskCmd, GroupID: 2}))
			taskListContainer.Refresh()
		}),
	)

	scrollArea := container.NewVScroll(taskListContainer)
	scrollArea.SetMinSize(fyne.NewSize(0, 400))

	// 4. 使用固定高度的滾動容器包裹狀態欄，防止其向上或向外撐開
	statusScroll := container.NewVScroll(statusLabel)
	statusScroll.SetMinSize(fyne.NewSize(0, 60)) // 固定狀態欄高度為 60

	bottomControls := container.NewVBox(
		widget.NewSeparator(),
		container.NewGridWithColumns(2,
			container.NewBorder(nil, nil, widget.NewLabel("順序(如1,2):"), nil, orderEntry),
			forceCheck,
		),
		container.NewPadded(syncBtn),
		statusScroll, // 放入滾動容器
	)

	mainLayout := container.NewVBox(
		container.NewPadded(widget.NewLabelWithStyle("任務編組池", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})),
		scrollArea,
		container.NewPadded(addBtnsRow),
		bottomControls,
	)

	window.SetOnClosed(func() {
		saveConfig(Config{Tasks: collectAllTasks(taskListContainer), GroupOrder: orderEntry.Text, ForceCopy: forceCheck.Checked})
	})

	go func() {
		for s := range statusChan {
			statusLabel.SetText("狀態: " + s)
		}
	}()

	window.SetContent(container.NewPadded(mainLayout))
	window.ShowAndRun()
}

// --- 其餘邏輯函數保持不變 ---
func executeCommand(command, dir string) {
	if command == "" {
		return
	}
	args := strings.Fields(command)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	statusChan <- "運行中: " + command
	_ = cmd.Run()
}
func collectAllTasks(c *fyne.Container) []TaskItem {
	var tasks []TaskItem
	for _, obj := range c.Objects {
		padded, ok := obj.(*fyne.Container)
		if !ok {
			continue
		}
		row := padded.Objects[0].(*fyne.Container)
		header := row.Objects[0].(*fyne.Container)
		gIDStr := header.Objects[1].(*widget.Entry).Text
		var gID int
		fmt.Sscanf(gIDStr, "%d", &gID)
		typeLabel := header.Objects[2].(*widget.Label).Text
		if strings.Contains(typeLabel, "同步") {
			grid := row.Objects[1].(*fyne.Container)
			src := grid.Objects[0].(*fyne.Container).Objects[0].(*widget.Entry).Text
			dst := grid.Objects[1].(*fyne.Container).Objects[0].(*widget.Entry).Text
			tasks = append(tasks, TaskItem{Type: TaskSync, GroupID: gID, Src: src, Dst: dst})
		} else {
			grid := row.Objects[1].(*fyne.Container)
			tasks = append(tasks, TaskItem{Type: TaskCmd, GroupID: gID, Root: grid.Objects[0].(*widget.Entry).Text, Cmd: grid.Objects[1].(*widget.Entry).Text, Desc: grid.Objects[2].(*widget.Entry).Text})
		}
	}
	return tasks
}
func fullSync(src, dst string, force bool) {
	_ = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if force {
			os.MkdirAll(filepath.Dir(target), 0755)
		}
		statusChan <- "同步: " + rel
		copyFile(path, target)
		return nil
	})
}
func copyFile(src, dst string) {
	s, err := os.Open(src)
	if err != nil {
		return
	}
	defer s.Close()
	d, err := os.Create(dst)
	if err != nil {
		return
	}
	defer d.Close()
	_, _ = io.Copy(d, s)
}
func loadConfig() Config {
	var c Config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{GroupOrder: "1,2"}
	}
	_ = json.Unmarshal(data, &c)
	return c
}
func saveConfig(c Config) {
	data, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(configPath, data, 0644)
}
