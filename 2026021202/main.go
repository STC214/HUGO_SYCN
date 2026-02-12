package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// --- 数据结构 ---
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
	window := myApp.NewWindow("Hugo 任务编组工具 V4.0")
	window.Resize(fyne.NewSize(900, 700))

	conf := loadConfig()
	statusLabel := widget.NewLabel("准备就绪")
	taskListContainer := container.NewVBox()

	// --- 核心：创建任务行的函数 ---

	// 创建同步行
	var createSyncRow func(TaskItem) fyne.CanvasObject
	createSyncRow = func(t TaskItem) fyne.CanvasObject {
		groupEntry := widget.NewEntry()
		groupEntry.SetText(fmt.Sprintf("%d", t.GroupID))
		srcEntry := widget.NewEntry()
		srcEntry.SetText(t.Src)
		dstEntry := widget.NewEntry()
		dstEntry.SetText(t.Dst)

		var row *fyne.Container
		removeBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			taskListContainer.Remove(row)
			taskListContainer.Refresh()
		})

		row = container.NewVBox(
			container.NewHBox(widget.NewLabel("分组ID:"), groupEntry, widget.NewLabel("【同步任务】")),
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
		return row
	}

	// 创建命令行
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

		var row *fyne.Container
		removeBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			taskListContainer.Remove(row)
			taskListContainer.Refresh()
		})

		row = container.NewVBox(
			container.NewHBox(widget.NewLabel("分组ID:"), groupEntry, widget.NewLabel("【脚本命令】")),
			container.NewGridWithColumns(3, rootEntry, cmdEntry, descEntry),
			container.NewHBox(widget.NewLabel("根目录 / 执行命令 / 按钮显示名"), removeBtn),
		)
		return row
	}

	// 初始化数据
	for _, t := range conf.Tasks {
		if t.Type == TaskSync {
			taskListContainer.Add(createSyncRow(t))
		} else {
			taskListContainer.Add(createCmdRow(t))
		}
	}

	// --- 底部控制区 ---
	orderEntry := widget.NewEntry()
	orderEntry.SetText(conf.GroupOrder)
	forceCheck := widget.NewCheck("强制覆盖模式", nil)
	forceCheck.Checked = conf.ForceCopy

	var syncBtn *widget.Button
	syncBtn = widget.NewButtonWithIcon("🔥 开始按顺序执行", theme.MediaPlayIcon(), func() {
		syncBtn.Disable()
		go func() {
			defer syncBtn.Enable()
			tasks := collectAllTasks(taskListContainer)
			orders := strings.Split(orderEntry.Text, ",")

			for _, gID := range orders {
				gID = strings.TrimSpace(gID)
				if gID == "" {
					continue
				}
				statusChan <- "正在运行组: " + gID

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
			statusChan <- "✅ 全部组任务执行完毕"
		}()
	})

	// --- 界面布局 ---
	addBtns := container.NewHBox(
		widget.NewButtonWithIcon("加同步对", theme.ContentAddIcon(), func() {
			taskListContainer.Add(createSyncRow(TaskItem{Type: TaskSync, GroupID: 1}))
			taskListContainer.Refresh() // 必须刷新！
		}),
		widget.NewButtonWithIcon("加命令行", theme.ContentAddIcon(), func() {
			taskListContainer.Add(createCmdRow(TaskItem{Type: TaskCmd, GroupID: 2}))
			taskListContainer.Refresh() // 必须刷新！
		}),
	)

	scrollArea := container.NewVScroll(taskListContainer)
	scrollArea.SetMinSize(fyne.NewSize(0, 400))

	mainLayout := container.NewVBox(
		widget.NewLabel("任务编组池:"),
		scrollArea,
		addBtns,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, widget.NewLabel("顺序(如1,2):"), forceCheck, orderEntry),
		syncBtn,
		statusLabel,
	)

	// 保存配置逻辑
	window.SetOnClosed(func() {
		saveConfig(Config{
			Tasks:      collectAllTasks(taskListContainer),
			GroupOrder: orderEntry.Text,
			ForceCopy:  forceCheck.Checked,
		})
	})

	// 状态更新线程
	go func() {
		for s := range statusChan {
			statusLabel.SetText("状态: " + s)
		}
	}()

	window.SetContent(mainLayout)
	window.ShowAndRun()
}

// --- 辅助逻辑 ---

func collectAllTasks(c *fyne.Container) []TaskItem {
	var tasks []TaskItem
	for _, obj := range c.Objects {
		row := obj.(*fyne.Container)
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
			tasks = append(tasks, TaskItem{
				Type: TaskCmd, GroupID: gID,
				Root: grid.Objects[0].(*widget.Entry).Text,
				Cmd:  grid.Objects[1].(*widget.Entry).Text,
				Desc: grid.Objects[2].(*widget.Entry).Text,
			})
		}
	}
	return tasks
}

func fullSync(src, dst string, force bool) {
	filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
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
	s, _ := os.Open(src)
	defer s.Close()
	d, _ := os.Create(dst)
	defer d.Close()
	io.Copy(d, s)
}

func executeCommand(command, dir string) {
	if command == "" {
		return
	}
	args := strings.Fields(command)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	statusChan <- "运行命令: " + command
	cmd.Run()
}

func loadConfig() Config {
	var c Config
	data, _ := os.ReadFile(configPath)
	json.Unmarshal(data, &c)
	return c
}

func saveConfig(c Config) {
	data, _ := json.Marshal(c)
	os.WriteFile(configPath, data, 0644)
}
