package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	sqdialog "github.com/sqweek/dialog" // 별칭 적용
	"golang.org/x/sys/windows"
)

// VMConfig 구조체 (CPU 관련 필드 추가됨)
type VMConfig struct {
	Name           string
	CPU            string
	CPUModel       string
	CPUCores       string
	CPUSockets     string
	CPUThreads     string
	CPUFeatures    string
	CPUAccel       string
	CPUAccelerator string
	RAM            string
	Disk           string
	GPU            string
	Network        string
	HW             string
}

type MemoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

// Windows 메모리 정보 조회
func globalMemoryStatusEx(ms *MemoryStatusEx) error {
	ms.Length = uint32(unsafe.Sizeof(*ms))
	ret, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(ms)))
	if ret == 0 {
		return err
	}
	return nil
}

func getTotalMemoryMB() uint64 {
	var ms MemoryStatusEx
	if err := globalMemoryStatusEx(&ms); err != nil {
		return 0
	}
	return ms.TotalPhys / (1024 * 1024)
}

// 디스크 파일 크기(가상 용량) MB 단위
func getDiskFileSizeMB(path string, diskType string) int64 {
	if _, err := os.Stat(path); err != nil {
		return 0
	}
	switch diskType {
	case "QCOW2":
		output, err := exec.Command("qemu-img", "info", "--output=json", path).Output()
		if err != nil {
			return 0
		}
		var info struct {
			VirtualSize int64 `json:"virtual-size"`
		}
		if err := json.Unmarshal(output, &info); err != nil {
			return 0
		}
		return info.VirtualSize / (1024 * 1024)
	case "RAW":
		if fi, err := os.Stat(path); err == nil {
			return fi.Size() / (1024 * 1024)
		}
		return 0
	case "VHD", "VMDK":
		output, err := exec.Command("qemu-img", "info", "--output=json", path).Output()
		if err != nil {
			return 0
		}
		var info struct {
			VirtualSize int64 `json:"virtual-size"`
		}
		if err := json.Unmarshal(output, &info); err != nil {
			return 0
		}
		return info.VirtualSize / (1024 * 1024)
	}
	return 0
}

// "QCOW2:E:\QEMU\disk.qcow2:10240" → (diskType, diskPath, diskCapacity)
func parseDiskInfo(diskInfo string) (string, string, string) {
	parts := strings.SplitN(diskInfo, ":", 2)
	if len(parts) < 2 {
		return "", "", ""
	}
	diskType := parts[0]
	rest := parts[1]
	idx := strings.LastIndex(rest, ":")
	if idx == -1 {
		return diskType, rest, ""
	}
	diskPath := rest[:idx]
	diskCapacity := rest[idx+1:]
	return diskType, diskPath, diskCapacity
}

// "vga=virtio,display=gtk" → map[vga:virtio display:gtk]
func parseGPUString(gpuStr string) map[string]string {
	result := make(map[string]string)
	if gpuStr == "" {
		return result
	}
	pairs := strings.Split(gpuStr, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

func EditVMConfig(vmName string, parent fyne.Window, onSave func()) {
	a := fyne.CurrentApp()
	winTitle := "가상머신 생성"
	if vmName != "" {
		winTitle = vmName + " 설정"
	}
	win := a.NewWindow(winTitle)
	win.Resize(fyne.NewSize(600, 400)) // 창 크기

	// 설정 파일 경로
	appData := os.Getenv("APPDATA")
	configDir := filepath.Join(appData, "goqemu")
	os.MkdirAll(configDir, os.ModePerm)

	config := &VMConfig{}
	if vmName != "" {
		configPath := filepath.Join(configDir, vmName+".conf")
		if data, err := ioutil.ReadFile(configPath); err == nil {
			lines := splitLines(string(data))
			for _, line := range lines {
				if len(line) == 0 {
					continue
				}
				parts := splitKeyValue(line)
				if len(parts) != 2 {
					continue
				}
				key, value := parts[0], parts[1]
				switch key {
				case "name":
					config.Name = value
				case "cpu":
					config.CPU = value
				case "cpuModel":
					config.CPUModel = value
				case "cpuCores":
					config.CPUCores = value
				case "cpuSockets":
					config.CPUSockets = value
				case "cpuThreads":
					config.CPUThreads = value
				case "cpuFeatures":
					config.CPUFeatures = value
				case "cpuAccel":
					config.CPUAccel = value
				case "cpuAccelerator":
					config.CPUAccelerator = value
				case "ram":
					config.RAM = value
				case "disk":
					config.Disk = value
				case "gpu":
					config.GPU = value
				case "network":
					config.Network = value
				case "hw":
					config.HW = value
				}
			}
		}
	}
	if vmName != "" {
		config.Name = vmName
	}

	// ─────────────────────────────────────────────
	// 기본정보
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("가상머신 이름")
	nameEntry.SetText(config.Name)

	// CPU
	cpuModels := []string{
		"Intel: Cascadelake-Server", "Intel: Skylake-Server/Client", "Intel: Broadwell",
		"Intel: Haswell", "Intel: IvyBridge", "Intel: SandyBridge", "Intel: Westmere",
		"Intel: Nehalem", "Intel: Penryn", "Intel: Conroe", "AMD: EPYC", "AMD: Opteron_G5",
		"AMD: Opteron_G4", "AMD: Opteron_G3", "AMD: Opteron_G2", "AMD: Opteron_G1", "Basic: qemu32",
		"Basic: qemu64", "ARM: Cortex-A57", "ARM: Cortex-M0", "ARM: Cortex-M4", "ARM: Cortex-M33",
		"MIPS: mips32r6-generic", "MIPS: P5600", "MIPS: M14K/M14Kc", "MIPS: 74Kf", "MIPS: 34Kf",
		"MIPS: 24Kc/24KEc/24Kf", "MIPS: 4Kc/4Km/4KEcR1/4KEmR1/4KEc/4KEm",
	}
	cpuModelSelect := widget.NewSelect(cpuModels, nil)
	cpuModelSelect.PlaceHolder = "CPU 모델 선택"
	if config.CPUModel != "" {
		cpuModelSelect.SetSelected(config.CPUModel)
	}

	cores := []string{"1", "2"}
	for i := 4; i <= 128; i += 2 {
		cores = append(cores, fmt.Sprintf("%d", i))
	}
	cpuCoresSelect := widget.NewSelect(cores, nil)
	cpuCoresSelect.PlaceHolder = "코어 수 선택"
	if config.CPUCores != "" {
		cpuCoresSelect.SetSelected(config.CPUCores)
	}

	sockets := []string{}
	for i := 1; i <= 16; i++ {
		sockets = append(sockets, fmt.Sprintf("%d", i))
	}
	cpuSocketsSelect := widget.NewSelect(sockets, nil)
	cpuSocketsSelect.PlaceHolder = "소켓 수 선택"
	if config.CPUSockets != "" {
		cpuSocketsSelect.SetSelected(config.CPUSockets)
	}

	cpuThreadsEntry := widget.NewEntry()
	cpuThreadsEntry.SetPlaceHolder("쓰레드 수 (예: 1)")
	cpuThreadsEntry.SetText(config.CPUThreads)

	cpuFeaturesEntry := widget.NewMultiLineEntry()
	cpuFeaturesEntry.SetPlaceHolder("추가 CPU 옵션 (예: +ssse3,-sse4.2)")
	cpuFeaturesEntry.SetText(config.CPUFeatures)

	acceleratorOptions := []string{"TCG", "KVM", "Xen", "hvf", "whpx", "nvmm"}
	acceleratorSelect := widget.NewSelect(acceleratorOptions, nil)
	acceleratorSelect.PlaceHolder = "가속기 선택"
	acceleratorSelect.Disable()

	cpuAccelCheck := widget.NewCheck("하드웨어 가속 사용", func(checked bool) {
		if checked {
			acceleratorSelect.Enable()
			if acceleratorSelect.Selected == "" {
				acceleratorSelect.SetSelected(acceleratorOptions[0])
			}
		} else {
			acceleratorSelect.Disable()
			acceleratorSelect.ClearSelected()
		}
	})

	if config.CPUAccel == "true" {
		cpuAccelCheck.SetChecked(true)
		acceleratorSelect.Enable()
		if config.CPUAccelerator != "" {
			acceleratorSelect.SetSelected(config.CPUAccelerator)
		} else {
			acceleratorSelect.SetSelected(acceleratorOptions[0])
		}
	} else {
		cpuAccelCheck.SetChecked(false)
		acceleratorSelect.Disable()
		acceleratorSelect.ClearSelected()
	}

	cpuPanel := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("CPU 모델", cpuModelSelect),
			widget.NewFormItem("코어 수", cpuCoresSelect),
			widget.NewFormItem("소켓 수", cpuSocketsSelect),
			widget.NewFormItem("쓰레드 수", cpuThreadsEntry),
			widget.NewFormItem("추가 옵션", cpuFeaturesEntry),
			widget.NewFormItem("가속기 설정", container.NewVBox(cpuAccelCheck, acceleratorSelect)),
		),
	)

	// RAM
	ramUnitSelect := widget.NewSelect([]string{"MB", "GB"}, nil)
	ramUnitSelect.PlaceHolder = "단위 선택"
	ramUnitSelect.SetSelected("MB")

	ramEntry := widget.NewEntry()
	ramEntry.SetPlaceHolder("용량 직접 입력 (숫자)")

	ramDropdown := widget.NewSelect([]string{}, nil)
	ramDropdown.PlaceHolder = "용량 선택"

	ramWarningLabel := widget.NewLabel("")
	ramWarningLabel.Wrapping = fyne.TextWrapWord

	generateRamOptions := func() []string {
		var maxVal int
		if ramUnitSelect.Selected == "MB" {
			maxVal = int(getTotalMemoryMB())
		} else {
			maxVal = int(getTotalMemoryMB() / 1024)
		}
		var step int
		if ramUnitSelect.Selected == "MB" {
			step = 256
		} else {
			step = 1
		}
		var options []string
		for val := step; val <= maxVal; val += step {
			options = append(options, fmt.Sprintf("%d", val))
		}
		return options
	}

	updateRamWarning := func() {
		if num, err := strconv.Atoi(ramEntry.Text); err == nil {
			var max int
			if ramUnitSelect.Selected == "MB" {
				max = int(getTotalMemoryMB())
			} else {
				max = int(getTotalMemoryMB() / 1024)
			}
			if num > max {
				num = max
				ramEntry.SetText(fmt.Sprintf("%d", num))
			}
			if float64(num) >= 0.8*float64(max) {
				ramWarningLabel.SetText("경고 : 가용 메모리의 80% 이상을 사용하려 합니다!")
				ramWarningLabel.Show()
			} else {
				ramWarningLabel.SetText("")
				ramWarningLabel.Hide()
			}
		} else {
			ramWarningLabel.SetText("")
			ramWarningLabel.Hide()
		}
		ramWarningLabel.Refresh()
	}

	updateRamDropdown := func() {
		options := generateRamOptions()
		ramDropdown.Options = options
		ramDropdown.Refresh()

		if val, err := strconv.Atoi(ramEntry.Text); err == nil {
			desired := fmt.Sprintf("%d", val)
			for _, opt := range options {
				if opt == desired {
					ramDropdown.SetSelected(desired)
					break
				}
			}
		}
		updateRamWarning()
	}

	ramUnitSelect.OnChanged = func(selected string) {
		updateRamDropdown()
		updateRamWarning()
	}
	ramEntry.OnChanged = func(text string) {
		updateRamDropdown()
		updateRamWarning()
	}
	ramDropdown.OnChanged = func(selected string) {
		ramEntry.SetText(selected)
		updateRamWarning()
	}
	updateRamDropdown()

	if config.RAM != "" {
		var numPart, unitPart string
		for i, ch := range config.RAM {
			if ch >= '0' && ch <= '9' {
				numPart += string(ch)
			} else {
				unitPart = config.RAM[i:]
				break
			}
		}
		if unitPart == "GB" || unitPart == "MB" {
			ramUnitSelect.SetSelected(unitPart)
		}
		ramEntry.SetText(numPart)
		updateRamDropdown()
		ramDropdown.SetSelected(numPart)
	}

	ramPanel := container.NewVBox(
		widget.NewLabel("RAM 설정"),
		container.NewHBox(widget.NewLabel("단위:"), ramUnitSelect),
		container.NewVBox(
			container.NewBorder(nil, nil, widget.NewLabel("직접 입력:"), nil, ramEntry),
		),
		container.NewHBox(widget.NewLabel("드롭다운:"), ramDropdown),
		ramWarningLabel,
	)

	// ─────────────────────────────────────────────
	// 하드디스크 탭
	fullAllocCheck := widget.NewCheck("디스크 공간 미리 할당", func(bool) {})

	var diskRows []*fyne.Container
	diskRowsContainer := container.NewVBox()

	// 스크롤 컨테이너로 감싸서 창이 넘칠 경우 스크롤
	diskScroll := container.NewScroll(diskRowsContainer)
	diskScroll.SetMinSize(fyne.NewSize(0, 200)) // 세로 200px 정도 공간

	addDiskButton := widget.NewButton("+", func() {
		diskNumber := len(diskRows) + 1
		label := widget.NewLabel(fmt.Sprintf("하드디스크 %d", diskNumber))
	
		diskTypes := []string{"QCOW2", "RAW", "VHD", "VMDK"}
		diskTypeSelect := widget.NewSelect(diskTypes, nil)
		diskTypeSelect.PlaceHolder = "디스크 종류 선택"
		diskTypeSelect.SetSelected(diskTypes[0]) // 기본값
	
		pathEntry := widget.NewEntry()
		pathEntry.SetPlaceHolder("경로 입력")
	
		capacityEntry := widget.NewEntry()
		capacityEntry.SetPlaceHolder("디스크 용량(MB)")
	
		var baseSizeMB int64
		var row *fyne.Container
	
		// "새로 생성" 버튼
		createBtn := widget.NewButton("경로선택", func() {
			path, err := sqdialog.File().Title("새 디스크 파일 생성").Save()
			if err != nil || path == "" {
				return
			}
			ext := filepath.Ext(path)
			if ext == "" {
				defaultExt := map[string]string{
					"QCOW2": ".qcow2",
					"RAW":   ".img",
					"VHD":   ".vhd",
					"VMDK":  ".vmdk",
				}
				if def, ok := defaultExt[diskTypeSelect.Selected]; ok {
					path += def
				}
			}
			pathEntry.SetText(path)
			// 파일 경로가 설정되었으므로, 디스크 타입 드롭다운 비활성화
			diskTypeSelect.Disable()
		})
	
		// "디스크 가져오기" 버튼
		loadBtn := widget.NewButton("디스크 가져오기", func() {
			path, err := sqdialog.File().Title("디스크 파일 가져오기").Load()
			if err != nil || path == "" {
				return
			}
			pathEntry.SetText(path)
			// 만약 파일 크기 확인 가능하다면
			if diskSize := getDiskFileSizeMB(path, diskTypeSelect.Selected); diskSize > 0 {
				baseSizeMB = diskSize
				capacityEntry.SetText(strconv.FormatInt(diskSize, 10))
			} else {
				baseSizeMB = 0
				capacityEntry.SetText("10240")
			}
			// 파일 경로가 설정되었으므로, 디스크 타입 드롭다운 비활성화
			diskTypeSelect.Disable()
		})
	
		removeBtn := widget.NewButton("-", func() {
			// 경로가 비어있으면 그냥 제거
			if pathEntry.Text == "" {
				for i, rowItem := range diskRows {
					if rowItem == row {
						diskRows = append(diskRows[:i], diskRows[i+1:]...)
						diskRowsContainer.Remove(row)
						diskRowsContainer.Refresh()
						break
					}
				}
				return
			}
			// 실제 경로가 있다면 Confirm dialog
			dialog.ShowConfirm("디스크 삭제", "디스크 파일도 삭제하시겠습니까?", func(deleteFile bool) {
				if deleteFile {
					if _, err := os.Stat(pathEntry.Text); err == nil {
						os.Remove(pathEntry.Text)
					}
				}
				for i, rowItem := range diskRows {
					if rowItem == row {
						diskRows = append(diskRows[:i], diskRows[i+1:]...)
						diskRowsContainer.Remove(row)
						diskRowsContainer.Refresh()
						break
					}
				}
			}, win)
		})
	
		capacityEntry.OnChanged = func(text string) {
			if baseSizeMB > 0 {
				if val, err := strconv.ParseInt(text, 10, 64); err == nil {
					if val < baseSizeMB {
						capacityEntry.SetText(strconv.FormatInt(baseSizeMB, 10))
					}
				}
			}
		}
	
		row = container.NewVBox(
			container.NewHBox(label, diskTypeSelect),
			container.NewBorder(nil, nil, container.NewHBox(createBtn, loadBtn, removeBtn), nil, pathEntry),
			widget.NewForm(
				widget.NewFormItem("용량(MB)", capacityEntry),
			),
		)
	
		diskRows = append(diskRows, row)
		diskRowsContainer.Add(row)
		diskRowsContainer.Refresh()
	})	

	// 기존 디스크 로드
	if config.Disk != "" {
		disks := strings.Split(config.Disk, ";")
		for _, diskInfo := range disks {
			dType, dPath, dCap := parseDiskInfo(diskInfo)
			if dType == "" && dPath == "" {
				continue
			}
	
			diskNumber := len(diskRows) + 1
			label := widget.NewLabel(fmt.Sprintf("하드디스크 %d", diskNumber))
	
			diskTypes := []string{"QCOW2", "RAW", "VHD", "VMDK"}
			diskTypeSelect := widget.NewSelect(diskTypes, nil)
			diskTypeSelect.PlaceHolder = "디스크 종류 선택"
			diskTypeSelect.SetSelected(dType)
	
			pathEntry := widget.NewEntry()
			pathEntry.SetPlaceHolder("경로 입력")
			pathEntry.SetText(dPath)
	
			capacityEntry := widget.NewEntry()
			capacityEntry.SetPlaceHolder("디스크 용량(MB)")
			capacityEntry.SetText(dCap)
	
			var baseSizeMB int64
			if stMB := getDiskFileSizeMB(dPath, dType); stMB > 0 {
				baseSizeMB = stMB
				if diskCapVal, err := strconv.ParseInt(dCap, 10, 64); err == nil && diskCapVal < stMB {
					capacityEntry.SetText(strconv.FormatInt(stMB, 10))
				}
			}
	
			capacityEntry.OnChanged = func(text string) {
				if baseSizeMB > 0 {
					val, err := strconv.ParseInt(text, 10, 64)
					if err == nil && val < baseSizeMB {
						capacityEntry.SetText(strconv.FormatInt(baseSizeMB, 10))
					}
				}
			}
	
			// 이미 dPath가 존재하므로, 디스크 타입 드롭다운을 비활성화
			if dPath != "" {
				diskTypeSelect.Disable()
			}
	
			var row *fyne.Container
			loadBtn := widget.NewButton("디스크 가져오기", func() {
				path, err := sqdialog.File().Title("디스크 파일 가져오기").Load()
				if err != nil || path == "" {
					return
				}
				pathEntry.SetText(path)
				if diskSize := getDiskFileSizeMB(path, diskTypeSelect.Selected); diskSize > 0 {
					baseSizeMB = diskSize
					capacityEntry.SetText(strconv.FormatInt(diskSize, 10))
				} else {
					baseSizeMB = 0
					capacityEntry.SetText("10240")
				}
				// 경로가 바뀌면(=새 파일을 불러오면) 다시 타입 변경 불가
				diskTypeSelect.Disable()
			})
			removeBtn := widget.NewButton("-", func() {
				if pathEntry.Text == "" {
					for i, rowItem := range diskRows {
						if rowItem == row {
							diskRows = append(diskRows[:i], diskRows[i+1:]...)
							diskRowsContainer.Remove(row)
							diskRowsContainer.Refresh()
							break
						}
					}
					return
				}
				dialog.ShowConfirm("디스크 삭제", "디스크 파일도 삭제하시겠습니까?", func(deleteFile bool) {
					if deleteFile {
						if _, err := os.Stat(pathEntry.Text); err == nil {
							os.Remove(pathEntry.Text)
						}
					}
					for i, rowItem := range diskRows {
						if rowItem == row {
							diskRows = append(diskRows[:i], diskRows[i+1:]...)
							diskRowsContainer.Remove(row)
							diskRowsContainer.Refresh()
							break
						}
					}
				}, win)
			})
	
			row = container.NewVBox(
				container.NewHBox(label, diskTypeSelect),
				container.NewBorder(nil, nil, container.NewHBox(loadBtn, removeBtn), nil, pathEntry),
				widget.NewForm(
					widget.NewFormItem("용량(MB)", capacityEntry),
				),
			)
	
			diskRows = append(diskRows, row)
			diskRowsContainer.Add(row)
		}
		diskRowsContainer.Refresh()
	}
	
	// header: +버튼, 체크박스 동일 너비
	headerGrid := container.NewGridWithColumns(2, addDiskButton, fullAllocCheck)
	header := container.NewVBox(
		widget.NewLabel("하드디스크 설정"),
		headerGrid,
	)
	diskPanel := container.NewBorder(header, nil, nil, nil, diskScroll)

	// GPU
	gpuFrontendOptions := []string{"cirrus", "std", "qxl", "virtio"}
	gpuDisplayOptions := []string{"gtk", "sdl", "vnc", "none"}
	gpuDeviceOptions := []string{"virtio-vga", "virtio-gpu", "virtio-gpu-gl", "vhost-user-vga", "vhost-user-gpu"}
	glOptions := []string{"off", "on"}
	gpuMemOptions := []string{"128M", "256M", "512M", "1G", "2G", "4G", "8G"}

	gpuFrontendSelect := widget.NewSelect(gpuFrontendOptions, nil)
	gpuFrontendSelect.PlaceHolder = "프론트엔드(-vga) 선택"

	gpuDisplaySelect := widget.NewSelect(gpuDisplayOptions, nil)
	gpuDisplaySelect.PlaceHolder = "디스플레이(-display)"

	gpuDeviceSelect := widget.NewSelect(gpuDeviceOptions, nil)
	gpuDeviceSelect.PlaceHolder = "VirtIO GPU 장치"

	glSelect := widget.NewSelect(glOptions, nil)
	glSelect.PlaceHolder = "GL 가속(on/off)"

	gpuMemSelect := widget.NewSelect(gpuMemOptions, nil)
	gpuMemSelect.PlaceHolder = "GPU 메모리(hostmem)"

	parsedGPU := parseGPUString(config.GPU)
	if val, ok := parsedGPU["vga"]; ok {
		gpuFrontendSelect.SetSelected(val)
	}
	if val, ok := parsedGPU["display"]; ok {
		gpuDisplaySelect.SetSelected(val)
	}
	if val, ok := parsedGPU["device"]; ok {
		gpuDeviceSelect.SetSelected(val)
	}
	if val, ok := parsedGPU["gl"]; ok {
		glSelect.SetSelected(val)
	}
	if val, ok := parsedGPU["hostmem"]; ok {
		gpuMemSelect.SetSelected(val)
	}

	gpuPanel := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("프론트엔드(vga)", gpuFrontendSelect),
			widget.NewFormItem("디스플레이", gpuDisplaySelect),
			widget.NewFormItem("VirtIO 장치", gpuDeviceSelect),
			widget.NewFormItem("GL 가속", glSelect),
			widget.NewFormItem("GPU 메모리(hostmem)", gpuMemSelect),
		),
	)

	// 네트워크, 하드웨어
	networkEntry := widget.NewEntry()
	networkEntry.SetPlaceHolder("네트워크 설정 (예: user)")
	networkEntry.SetText(config.Network)

	hwEntry := widget.NewMultiLineEntry()
	hwEntry.SetPlaceHolder("하드웨어 설정 (커널, 바이오스, 디스크 파일 등)")
	hwEntry.SetText(config.HW)

	rightPanel := container.NewMax()

	updateConfigFromEntries := func() {
		config.Name = nameEntry.Text
		config.CPUModel = cpuModelSelect.Selected
		config.CPUCores = cpuCoresSelect.Selected
		config.CPUSockets = cpuSocketsSelect.Selected
		config.CPUThreads = cpuThreadsEntry.Text
		config.CPUFeatures = cpuFeaturesEntry.Text
		config.CPUAccel = strconv.FormatBool(cpuAccelCheck.Checked)
		if cpuAccelCheck.Checked && acceleratorSelect.Selected == "" {
			acceleratorSelect.SetSelected(acceleratorOptions[0])
		}
		config.CPUAccelerator = acceleratorSelect.Selected
		config.RAM = ramEntry.Text + ramUnitSelect.Selected

		var disks []string
		for _, row := range diskRows {
			if len(row.Objects) < 2 {
				continue
			}
			hbox, ok := row.Objects[0].(*fyne.Container)
			if !ok || len(hbox.Objects) < 2 {
				continue
			}
			diskTypeSelect, ok := hbox.Objects[1].(*widget.Select)
			if !ok {
				continue
			}
			dType := diskTypeSelect.Selected

			border, ok := row.Objects[1].(*fyne.Container)
			if !ok {
				continue
			}
			var pathEntry *widget.Entry
			for _, obj := range border.Objects {
				if entry, ok2 := obj.(*widget.Entry); ok2 {
					pathEntry = entry
					break
				}
			}
			var capacityEntry *widget.Entry
			if len(row.Objects) > 2 {
				if formObj, ok := row.Objects[2].(*widget.Form); ok {
					if len(formObj.Items) > 0 {
						if e, ok2 := formObj.Items[0].Widget.(*widget.Entry); ok2 {
							capacityEntry = e
						}
					}
				}
			}
			if pathEntry != nil && pathEntry.Text != "" {
				dPath := pathEntry.Text
				dCap := ""
				if capacityEntry != nil {
					dCap = capacityEntry.Text
				}
				disks = append(disks, dType+":"+dPath+":"+dCap)
			}
		}
		if len(disks) > 0 {
			config.Disk = strings.Join(disks, ";")
		} else {
			config.Disk = ""
		}

		var gpuPairs []string
		if gpuFrontendSelect.Selected != "" {
			gpuPairs = append(gpuPairs, "vga="+gpuFrontendSelect.Selected)
		}
		if gpuDisplaySelect.Selected != "" && gpuDisplaySelect.Selected != "none" {
			gpuPairs = append(gpuPairs, "display="+gpuDisplaySelect.Selected)
		}
		if gpuDeviceSelect.Selected != "" {
			gpuPairs = append(gpuPairs, "device="+gpuDeviceSelect.Selected)
		}
		if glSelect.Selected != "" && glSelect.Selected != "off" {
			gpuPairs = append(gpuPairs, "gl="+glSelect.Selected)
		}
		if gpuMemSelect.Selected != "" {
			gpuPairs = append(gpuPairs, "hostmem="+gpuMemSelect.Selected)
		}
		config.GPU = strings.Join(gpuPairs, ",")

		config.Network = networkEntry.Text
		config.HW = hwEntry.Text
	}

	setRightPanel := func(content fyne.CanvasObject) {
		rightPanel.Objects = []fyne.CanvasObject{content}
		rightPanel.Refresh()
	}

	basicPanel := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("이름", nameEntry),
		),
	)
	networkPanel := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("네트워크", networkEntry),
		),
	)
	hwPanel := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("하드웨어", hwEntry),
		),
	)

	btnBasic := widget.NewButton("기본정보", func() { setRightPanel(basicPanel) })
	btnCPU := widget.NewButton("CPU", func() { setRightPanel(cpuPanel) })
	btnRAM := widget.NewButton("RAM", func() { setRightPanel(ramPanel) })
	btnDisk := widget.NewButton("하드디스크", func() { setRightPanel(diskPanel) })
	btnGPU := widget.NewButton("GPU", func() { setRightPanel(gpuPanel) })
	btnNetwork := widget.NewButton("네트워크", func() { setRightPanel(networkPanel) })
	btnHW := widget.NewButton("하드웨어", func() { setRightPanel(hwPanel) })
	leftPanel := container.NewVBox(btnBasic, btnCPU, btnRAM, btnDisk, btnGPU, btnNetwork, btnHW)

	setRightPanel(basicPanel)

	saveBtn := widget.NewButton("저장", func() {
		updateConfigFromEntries()
		if config.Name == "" {
			dialog.ShowError(errEmptyName(), win)
			return
		}
		if vmName != "" && vmName != config.Name {
			oldPath := filepath.Join(configDir, vmName+".conf")
			newPath := filepath.Join(configDir, config.Name+".conf")
			if err := os.Rename(oldPath, newPath); err != nil {
				dialog.ShowError(err, win)
				return
			}
		}

		// 최종 저장 시, 없는 디스크 파일은 qemu-img create
		disks := strings.Split(config.Disk, ";")
		for _, diskInfo := range disks {
			dType, dPath, dCap := parseDiskInfo(diskInfo)
			if dPath == "" {
				continue
			}
			if _, err := os.Stat(dPath); os.IsNotExist(err) {
				capacityVal, err := strconv.ParseInt(dCap, 10, 64)
				if err != nil || capacityVal < 1 {
					capacityVal = 10240
				}
				formatMap := map[string]string{
					"QCOW2": "qcow2",
					"RAW":   "raw",
					"VHD":   "vpc",
					"VMDK":  "vmdk",
				}
				format := formatMap[dType]
				if format == "" {
					format = "qcow2"
				}

				cmdArgs := []string{"create", "-f", format, dPath, fmt.Sprintf("%dM", capacityVal)}
				if fullAllocCheck.Checked && format == "qcow2" {
					cmdArgs = append(cmdArgs, "-o", "preallocation=full")
				}
				cmd := exec.Command("qemu-img", cmdArgs...)
				if err2 := cmd.Run(); err2 != nil {
					// fallback
					fallbackArgs := []string{"create", "-f", format, dPath, fmt.Sprintf("%dM", capacityVal)}
					fallbackCmd := exec.Command("qemu-img", fallbackArgs...)
					if err3 := fallbackCmd.Run(); err3 != nil {
						dialog.ShowError(fmt.Errorf("preallocation=full 실패 후 fallback도 실패: %v", err3), win)
						return
					} else {
						dialog.ShowInformation("경고", "preallocation=full이 실패하여 기본 방식으로 생성했습니다.", win)
					}
				}
			}
		}

		configContent := "name=" + config.Name + "\n" +
			"cpuModel=" + config.CPUModel + "\n" +
			"cpuCores=" + config.CPUCores + "\n" +
			"cpuSockets=" + config.CPUSockets + "\n" +
			"cpuThreads=" + config.CPUThreads + "\n" +
			"cpuFeatures=" + config.CPUFeatures + "\n" +
			"cpuAccel=" + config.CPUAccel + "\n" +
			"cpuAccelerator=" + config.CPUAccelerator + "\n" +
			"ram=" + config.RAM + "\n" +
			"disk=" + config.Disk + "\n" +
			"gpu=" + config.GPU + "\n" +
			"network=" + config.Network + "\n" +
			"hw=" + config.HW + "\n"

		if err := ioutil.WriteFile(filepath.Join(configDir, config.Name+".conf"), []byte(configContent), 0644); err != nil {
			dialog.ShowError(err, win)
		} else {
			dialog.ShowInformation("저장", "설정이 저장되었습니다.", parent)
			win.Close()
			if onSave != nil {
				onSave()
			}
		}
	})
	cancelBtn := widget.NewButton("취소", func() {
		win.Close()
	})
	bottomBar := container.NewHBox(saveBtn, cancelBtn)

	split := container.NewHSplit(leftPanel, rightPanel)
	split.SetOffset(0.2)
	content := container.NewBorder(nil, bottomBar, nil, nil, split)

	win.SetContent(content)
	win.CenterOnScreen()
	win.Show()
}

// 줄바꿈(\n, \r\n) 지원
func splitLines(s string) []string {
	var lines []string
	tmp := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, tmp)
			tmp = ""
		} else if r != '\r' {
			tmp += string(r)
		}
	}
	if tmp != "" {
		lines = append(lines, tmp)
	}
	return lines
}

// "key=value" 분할
func splitKeyValue(s string) []string {
	for i, ch := range s {
		if ch == '=' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{}
}

func errEmptyName() error {
	return &emptyNameError{}
}

type emptyNameError struct{}

func (e *emptyNameError) Error() string {
	return "가상머신 이름을 입력하십시오."
}
