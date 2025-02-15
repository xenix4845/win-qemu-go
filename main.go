package main

import (
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("Go-QEMU VMM")
	w.Resize(fyne.NewSize(800, 600))

	appData := os.Getenv("APPDATA")
	configDir := filepath.Join(appData, "goqemu")
	os.MkdirAll(configDir, os.ModePerm)
	configs := loadVMConfigs(configDir)

	vmList := widget.NewList(
		func() int { return len(configs) },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			// 예: "VM 이름 (CPU 모델)" 형태로 표시
			o.(*widget.Label).SetText(configs[i].Name + " (" + configs[i].CPUModel + ")")
		},
	)

	// 함수: 리스트를 새로 읽어오고 refresh 처리
	refreshVMList := func() {
		configs = loadVMConfigs(configDir)
		vmList.Refresh()
	}

	// 왼쪽 패널: 관리 버튼 영역 (전체 너비의 1/5 차지)
	createBtn := widget.NewButton("가상머신 생성", func() {
		// 저장 콜백 전달하여 생성 후 자동 갱신
		EditVMConfig("", w, refreshVMList)
	})
	managementPanel := container.NewVBox(createBtn)

	// vmList 항목 클릭 시 관리창 코드 수정 (삭제 버튼 추가)
	vmList.OnSelected = func(id widget.ListItemID) {
		config := configs[id]
		ctrlWin := a.NewWindow(config.Name + " 관리")
		startBtn := widget.NewButton("시작", func() {
			dialog.ShowInformation("시작", config.Name+" 가상머신을 시작합니다.", w)
			ctrlWin.Close()
		})
		settingBtn := widget.NewButton("설정", func() {
			EditVMConfig(config.Name, w, refreshVMList)
			ctrlWin.Close()
		})
		deleteBtn := widget.NewButton("삭제", func() {
			// 별도의 삭제 확인 창 생성
			confirmWin := a.NewWindow("삭제 확인")
			confirmLabel := widget.NewLabel(config.Name + " 가상머신을 삭제하시겠습니까?")
			yesBtn := widget.NewButton("네", func() {
				configPath := filepath.Join(configDir, config.Name+".conf")
				if err := os.Remove(configPath); err != nil {
					dialog.ShowError(err, confirmWin)
				} else {
					dialog.ShowInformation("삭제", config.Name+" 가상머신이 삭제되었습니다.", confirmWin)
				}
				refreshVMList()
				ctrlWin.Close()    // 관리창 닫기
				confirmWin.Close() // 삭제 확인창 닫기
			})
			noBtn := widget.NewButton("아니오", func() {
				confirmWin.Close()
			})
			confirmWin.SetContent(
				container.NewVBox(
					confirmLabel,
					container.NewHBox(yesBtn, noBtn),
				),
			)
			confirmWin.Resize(fyne.NewSize(300, 100))
			confirmWin.CenterOnScreen()
			confirmWin.Show()
		})
		closeBtn := widget.NewButton("닫기", func() {
			ctrlWin.Close()
		})
		ctrlWin.SetContent(
			container.NewVBox(
				widget.NewLabel(config.Name+" 가상머신"),
				container.NewHBox(settingBtn, deleteBtn, startBtn, closeBtn),
			),
		)
		ctrlWin.Resize(fyne.NewSize(300, 100))
		ctrlWin.CenterOnScreen() // 관리 창을 정가운데에 표시
		ctrlWin.Show()
		vmList.Unselect(id)
	}

	// 좌측 관리 패널과 우측 리스트를 분할 레이아웃으로 표시 (왼쪽 20%)
	split := container.NewHSplit(managementPanel, vmList)
	split.SetOffset(0.2)

	w.SetContent(split)
	w.CenterOnScreen() // 메인 창을 정가운데에 표시
	w.ShowAndRun()
}
