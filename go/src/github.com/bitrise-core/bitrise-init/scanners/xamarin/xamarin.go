package xamarin

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"

	log "github.com/Sirupsen/logrus"
	"github.com/bitrise-core/bitrise-init/models"
	"github.com/bitrise-core/bitrise-init/steps"
	"github.com/bitrise-core/bitrise-init/utility"
	bitriseModels "github.com/bitrise-io/bitrise/models"
	envmanModels "github.com/bitrise-io/envman/models"
	"github.com/bitrise-io/go-utils/fileutil"
)

const (
	scannerName = "xamarin"
)

const (
	solutionExtension          = ".sln"
	solutionConfigurationStart = "GlobalSection(SolutionConfigurationPlatforms) = preSolution"
	solutionConfigurationEnd   = "EndGlobalSection"
	projectTypeGUIDExp         = `(?i)<ProjectTypeGuids>(?P<project_type_guids>.*)<\/ProjectTypeGuids>`

	includeMonoTouchAPIPattern   = `Include="monotouch"`
	includeXamarinIosAPIPattern  = `Include="Xamarin.iOS"`
	includeMonoAndroidAPIPattern = `Include="Mono.Android"`

	monoTouchAPI   = "monotouch"
	xamarinIosAPI  = "Xamarin.iOS"
	monoAndroidAPI = "Mono.Android"

	includeXamarinUITestFrameworkPattern = `Include="Xamarin.UITest`
	includeNunitFrameworkPattern         = `Include="nunit.framework`
	includeNunitLiteFrameworkExp         = `(?i)Include=".*.NUnitLite"`

	xamarinUITestType = "Xamarin UITest"
	nunitTestType     = "NUnit test"
	nunitLiteTestType = "NUnitLite test"
)

const (
	xamarinProjectKey    = "xamarin_project"
	xamarinProjectTitle  = "Path to Xamarin Solution"
	xamarinProjectEnvKey = "BITRISE_PROJECT_PATH"

	xamarinConfigurationKey    = "xamarin_configuration"
	xamarinConfigurationTitle  = "Xamarin project configuration"
	xamarinConfigurationEnvKey = "BITRISE_XAMARIN_CONFIGURATION"

	xamarinPlatformKey    = "xamarin_platform"
	xamarinPlatformTitle  = "Xamarin platform"
	xamarinPlatformEnvKey = "BITRISE_XAMARIN_PLATFORM"

	xamarinIosLicenceKey   = "xamarin_ios_license"
	xamarinIosLicenceTitle = "Xamarin.iOS License"

	xamarinAndroidLicenceKey   = "xamarin_android_license"
	xamarinAndroidLicenceTitle = "Xamarin.Android License"

	xamarinMacLicenseKey   = "xamarin_mac_license"
	xamarinMacLicenseTitle = "Xamarin.Mac License"
)

var (
	logger = utility.NewLogger()
)

var (
	projectTypeGUIDMap = map[string][]string{
		"Xamarin.iOS": []string{
			"E613F3A2-FE9C-494F-B74E-F63BCB86FEA6",
			"6BC8ED88-2882-458C-8E55-DFD12B67127B",
			"F5B4F3BC-B597-4E2B-B552-EF5D8A32436F",
			"FEACFBD2-3405-455C-9665-78FE426C6842",
			"8FFB629D-F513-41CE-95D2-7ECE97B6EEEC",
			"EE2C853D-36AF-4FDB-B1AD-8E90477E2198",
		},
		"Xamarin.Android": []string{
			"EFBA0AD7-5A72-4C68-AF49-83D382785DCF",
			"10368E6C-D01B-4462-8E8B-01FC667A7035",
		},
		"MonoMac": []string{
			"1C533B1C-72DD-4CB1-9F6B-BF11D93BCFBE",
			"948B3504-5B70-4649-8FE4-BDE1FB46EC69",
		},
		"Xamarin.Mac": []string{
			"42C0BBD9-55CE-4FC1-8D90-A7348ABAFB23",
			"A3F8F2AB-B479-4A4A-A458-A89E7DC349F1",
		},
		"Xamarin.tvOS": []string{
			"06FA79CB-D6CD-4721-BB4B-1BD202089C55",
		},
	}
)

//--------------------------------------------------
// Utility
//--------------------------------------------------

func projectType(guids []string) string {
	for _, GUID := range guids {
		for projectType, projectTypeGUIDs := range projectTypeGUIDMap {
			for _, aGUID := range projectTypeGUIDs {
				if aGUID == GUID {
					return projectType
				}
			}
		}
	}

	return ""
}

func filterSolutionFiles(fileList []string) []string {
	files := utility.FilterFilesWithExtensions(fileList, solutionExtension)

	componentsProjectExp := regexp.MustCompile(`.*Components/.+.sln`)

	relevantFiles := []string{}
	for _, file := range files {
		isComponentsSolution := false
		if componentsProjectExp.FindString(file) != "" {
			isComponentsSolution = true
		}

		if !isComponentsSolution {
			relevantFiles = append(relevantFiles, file)
		}
	}

	sort.Sort(utility.ByComponents(relevantFiles))
	return relevantFiles
}

func getSolutionConfigs(solutionFile string) (map[string][]string, error) {
	content, err := fileutil.ReadStringFromFile(solutionFile)
	if err != nil {
		return map[string][]string{}, err
	}

	configMap := map[string][]string{}
	isNextLineScheme := false

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, solutionConfigurationStart) {
			isNextLineScheme = true
			continue
		}

		if strings.Contains(line, solutionConfigurationEnd) {
			isNextLineScheme = false
			continue
		}

		if isNextLineScheme {
			split := strings.Split(line, "=")
			if len(split) == 2 {
				configCompositStr := strings.TrimSpace(split[1])
				configSplit := strings.Split(configCompositStr, "|")

				if len(configSplit) == 2 {
					config := configSplit[0]
					platform := configSplit[1]

					platforms := configMap[config]
					platforms = append(platforms, platform)

					configMap[config] = platforms
				}
			} else {
				return map[string][]string{}, fmt.Errorf("failed to parse config line (%s)", line)
			}
		}
	}

	return configMap, nil
}

func getProjectGUIDs(projectFile string) ([]string, error) {
	projectTypeGUIDSExp := regexp.MustCompile(projectTypeGUIDExp)
	content, err := fileutil.ReadStringFromFile(projectFile)
	if err != nil {
		return []string{}, err
	}

	lines := strings.Split(content, "\n")
	guidsStr := ""
	for _, line := range lines {
		match := projectTypeGUIDSExp.FindStringSubmatch(line)
		if len(match) == 2 {
			guidsStr = match[1]
		}
	}

	guids := []string{}
	guidsSplit := strings.Split(guidsStr, ";")
	for _, guidStr := range guidsSplit {
		guid := guidStr
		if strings.HasPrefix(guid, "{") {
			guid = strings.TrimPrefix(guid, "{")
		}

		if strings.HasSuffix(guid, "}") {
			guid = strings.TrimSuffix(guid, "}")
		}

		guids = append(guids, guid)
	}

	return guids, nil
}

func getProjectTestType(projectFile string) (string, error) {
	content, err := fileutil.ReadStringFromFile(projectFile)
	if err != nil {
		return "", err
	}

	return projectTestType(content), nil
}

func projectTestType(projectFileContent string) string {
	if utility.CaseInsensitiveContains(projectFileContent, includeXamarinUITestFrameworkPattern) {
		return xamarinUITestType
	} else if utility.CaseInsensitiveContains(projectFileContent, includeNunitFrameworkPattern) {
		return nunitTestType
	} else {
		lines := strings.Split(projectFileContent, string(filepath.Separator))
		nunitLiteExp := regexp.MustCompile(includeNunitLiteFrameworkExp)

		for _, line := range lines {
			if nunitLiteExp.FindString(line) != "" {
				return nunitLiteTestType
			}
		}
	}

	return ""
}

func getProjects(solutionFile string) ([]string, error) {
	content, err := fileutil.ReadStringFromFile(solutionFile)
	if err != nil {
		return []string{}, err
	}

	projectDir := filepath.Dir(solutionFile)
	projectExp := regexp.MustCompile(`Project\(\"[^\"]*\"\)\s*=\s*\"[^\"]*\",\s*\"([^\"]*.csproj)\"`)

	projects := []string{}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		matches := projectExp.FindStringSubmatch(line)
		if len(matches) > 1 {
			project := strings.Replace(matches[1], "\\", "/", -1)
			projectPath := path.Join(projectDir, project)
			projects = append(projects, projectPath)
		}
	}

	return projects, nil
}

func configName(hasNugetPackages, hasXamarinComponents bool) string {
	name := "xamarin-"
	if hasNugetPackages {
		name = name + "nuget-"
	}
	if hasXamarinComponents {
		name = name + "components-"
	}
	return name + "config"
}

func defaultConfigName() string {
	return "default-xamarin-config"
}

//--------------------------------------------------
// Scanner
//--------------------------------------------------

// Scanner ...
type Scanner struct {
	SearchDir     string
	FileList      []string
	SolutionFiles []string

	HasNugetPackages     bool
	HasXamarinComponents bool

	HasIosProject     bool
	HasAndroidProject bool
	HasMacProject     bool
	HasTVOSProject    bool
}

// Name ...
func (scanner Scanner) Name() string {
	return scannerName
}

// Configure ...
func (scanner *Scanner) Configure(searchDir string) {
	scanner.SearchDir = searchDir
}

// DetectPlatform ...
func (scanner *Scanner) DetectPlatform() (bool, error) {
	fileList, err := utility.FileList(scanner.SearchDir)
	if err != nil {
		return false, fmt.Errorf("failed to search for files in (%s), error: %s", scanner.SearchDir, err)
	}
	scanner.FileList = fileList

	// Search for solution file
	logger.Info("Searching for solution files")

	solutionFiles := filterSolutionFiles(fileList)
	scanner.SolutionFiles = solutionFiles

	logger.InfofDetails("%d solution file(s) detected:", len(solutionFiles))
	for _, file := range solutionFiles {
		logger.InfofDetails("  - %s", file)
	}

	if len(solutionFiles) == 0 {
		logger.InfofDetails("platform not detected")
		return false, nil
	}

	logger.InfofReceipt("platform detected")

	return true, nil
}

// Options ...
func (scanner *Scanner) Options() (models.OptionModel, models.Warnings, error) {
	logger.InfoSection("Searching for Nuget packages & Xamarin Components")

	warnings := models.Warnings{}

	for _, file := range scanner.FileList {
		// Search for nuget packages
		if scanner.HasNugetPackages == false {
			baseName := filepath.Base(file)
			if baseName == "packages.config" {
				scanner.HasNugetPackages = true
			}
		}

		// If adding a component:
		// /Components/[COMPONENT_NAME]/ dir added
		// ItemGroup/XamarinComponentReference added to the project
		// packages.config added to the project's folder
		if scanner.HasXamarinComponents == false {
			componentsExp := regexp.MustCompile(".*Components/.+")
			if result := componentsExp.FindString(file); result != "" {
				scanner.HasXamarinComponents = true
			}
		}

		if scanner.HasNugetPackages && scanner.HasXamarinComponents {
			break
		}
	}

	if scanner.HasNugetPackages {
		logger.InfofReceipt("Nuget packages found")
	} else {
		logger.InfofDetails("NO Nuget packages found")
	}

	if scanner.HasXamarinComponents {
		logger.InfofReceipt("Xamarin Components found")
	} else {
		logger.InfofDetails("NO Xamarin Components found")
	}

	// Check for solution configs
	validSolutionMap := map[string]map[string][]string{}
	for _, solutionFile := range scanner.SolutionFiles {
		logger.InfofSection("Inspecting solution file: %s", solutionFile)

		configs, err := getSolutionConfigs(solutionFile)
		if err != nil {
			return models.OptionModel{}, models.Warnings{}, err
		}

		if len(configs) > 0 {
			logger.InfofReceipt("found configs: %v", configs)

			validSolutionMap[solutionFile] = configs
		} else {
			log.Warnf("No config found for %s", solutionFile)
		}
	}

	if len(validSolutionMap) == 0 {
		return models.OptionModel{}, models.Warnings{}, errors.New("No valid solution file found")
	}

	// Check for solution projects
	xamarinProjectOption := models.NewOptionModel(xamarinProjectTitle, xamarinProjectEnvKey)

	for solutionFile, configMap := range validSolutionMap {
		projects, err := getProjects(solutionFile)
		if err != nil {
			return models.OptionModel{}, models.Warnings{}, err
		}

		// Inspect projects
		for _, project := range projects {
			logger.InfofSection("  Inspecting project file: %s", project)

			guids, err := getProjectGUIDs(project)
			if err != nil {
				return models.OptionModel{}, models.Warnings{}, err
			}

			projectType := projectType(guids)

			testType, err := getProjectTestType(project)
			if err != nil {
				return models.OptionModel{}, models.Warnings{}, err
			}

			if testType != "" {
				logger.InfofReceipt("    test project type: %s", testType)
				continue
			}

			if projectType == "" {
				log.Warn("    No platform api or test framework found")
				continue
			}

			if projectType == "Xamarin.iOS" {
				scanner.HasIosProject = true
			} else if projectType == "Xamarin.Android" {
				scanner.HasAndroidProject = true
			} else if projectType == "MonoMac" || projectType == "Xamarin.Mac" {
				scanner.HasMacProject = true
			} else if projectType == "Xamarin.tvOS" {
				scanner.HasTVOSProject = true
			} else {
				log.Warn("    Unknow project type for GUIDs: %v", guids)
				continue
			}

			logger.InfofReceipt("    project type: %s", projectType)
		}

		xamarinConfigurationOption := models.NewOptionModel(xamarinConfigurationTitle, xamarinConfigurationEnvKey)

		for config, platforms := range configMap {
			xamarinPlatformOption := models.NewOptionModel(xamarinPlatformTitle, xamarinPlatformEnvKey)
			for _, platform := range platforms {
				configOption := models.NewEmptyOptionModel()
				configOption.Config = configName(scanner.HasNugetPackages, scanner.HasXamarinComponents)

				xamarinPlatformOption.ValueMap[platform] = configOption
			}

			xamarinConfigurationOption.ValueMap[config] = xamarinPlatformOption
		}

		xamarinProjectOption.ValueMap[solutionFile] = xamarinConfigurationOption
	}

	return xamarinProjectOption, warnings, nil
}

// DefaultOptions ...
func (scanner *Scanner) DefaultOptions() models.OptionModel {
	configOption := models.NewEmptyOptionModel()
	configOption.Config = defaultConfigName()

	xamarinProjectOption := models.NewOptionModel(xamarinProjectTitle, xamarinProjectEnvKey)
	xamarinConfigurationOption := models.NewOptionModel(xamarinConfigurationTitle, xamarinConfigurationEnvKey)
	xamarinPlatformOption := models.NewOptionModel(xamarinPlatformTitle, xamarinPlatformEnvKey)

	xamarinPlatformOption.ValueMap["_"] = configOption
	xamarinConfigurationOption.ValueMap["_"] = xamarinPlatformOption
	xamarinProjectOption.ValueMap["_"] = xamarinConfigurationOption

	return xamarinProjectOption
}

// Configs ...
func (scanner *Scanner) Configs() (models.BitriseConfigMap, error) {
	stepList := []bitriseModels.StepListItemModel{}

	// ActivateSSHKey
	stepList = append(stepList, steps.ActivateSSHKeyStepListItem())

	// GitClone
	stepList = append(stepList, steps.GitCloneStepListItem())

	// Script
	stepList = append(stepList, steps.ScriptSteplistItem())

	// CertificateAndProfileInstaller
	stepList = append(stepList, steps.CertificateAndProfileInstallerStepListItem())

	// XamarinUserManagement
	inputs := []envmanModels.EnvironmentItemModel{}
	if scanner.HasIosProject {
		inputs = append(inputs, envmanModels.EnvironmentItemModel{xamarinIosLicenceKey: "yes"})
	}
	if scanner.HasAndroidProject {
		inputs = append(inputs, envmanModels.EnvironmentItemModel{xamarinAndroidLicenceKey: "yes"})
	}
	if scanner.HasMacProject {
		inputs = append(inputs, envmanModels.EnvironmentItemModel{xamarinMacLicenseKey: "yes"})
	}

	stepList = append(stepList, steps.XamarinUserManagementStepListItem(inputs))

	// NugetRestore
	if scanner.HasNugetPackages {
		stepList = append(stepList, steps.NugetRestoreStepListItem())
	}

	// XamarinComponentsRestore
	if scanner.HasXamarinComponents {
		stepList = append(stepList, steps.XamarinComponentsRestoreStepListItem())
	}

	// XamarinBuilder
	inputs = []envmanModels.EnvironmentItemModel{
		envmanModels.EnvironmentItemModel{xamarinProjectKey: "$" + xamarinProjectEnvKey},
		envmanModels.EnvironmentItemModel{xamarinConfigurationKey: "$" + xamarinConfigurationEnvKey},
		envmanModels.EnvironmentItemModel{xamarinPlatformKey: "$" + xamarinPlatformEnvKey},
	}

	stepList = append(stepList, steps.XamarinBuilderStepListItem(inputs))

	// DeployToBitriseIo
	stepList = append(stepList, steps.DeployToBitriseIoStepListItem())

	bitriseData := models.BitriseDataWithPrimaryWorkflowSteps(stepList)
	data, err := yaml.Marshal(bitriseData)
	if err != nil {
		return models.BitriseConfigMap{}, err
	}

	configName := configName(scanner.HasNugetPackages, scanner.HasXamarinComponents)
	bitriseDataMap := models.BitriseConfigMap{
		configName: string(data),
	}

	return bitriseDataMap, nil
}

// DefaultConfigs ...
func (scanner *Scanner) DefaultConfigs() (models.BitriseConfigMap, error) {
	stepList := []bitriseModels.StepListItemModel{}

	// ActivateSSHKey
	stepList = append(stepList, steps.ActivateSSHKeyStepListItem())

	// GitClone
	stepList = append(stepList, steps.GitCloneStepListItem())

	// Script
	stepList = append(stepList, steps.ScriptSteplistItem())

	// CertificateAndProfileInstaller
	stepList = append(stepList, steps.CertificateAndProfileInstallerStepListItem())

	// XamarinUserManagement
	inputs := []envmanModels.EnvironmentItemModel{}
	stepList = append(stepList, steps.XamarinUserManagementStepListItem(inputs))

	// NugetRestore
	stepList = append(stepList, steps.NugetRestoreStepListItem())

	// XamarinComponentsRestore
	stepList = append(stepList, steps.XamarinComponentsRestoreStepListItem())

	// XamarinBuilder
	inputs = []envmanModels.EnvironmentItemModel{
		envmanModels.EnvironmentItemModel{xamarinProjectKey: "$" + xamarinProjectEnvKey},
		envmanModels.EnvironmentItemModel{xamarinConfigurationKey: "$" + xamarinConfigurationEnvKey},
		envmanModels.EnvironmentItemModel{xamarinPlatformKey: "$" + xamarinPlatformEnvKey},
	}

	stepList = append(stepList, steps.XamarinBuilderStepListItem(inputs))

	// DeployToBitriseIo
	stepList = append(stepList, steps.DeployToBitriseIoStepListItem())

	bitriseData := models.BitriseDataWithPrimaryWorkflowSteps(stepList)
	data, err := yaml.Marshal(bitriseData)
	if err != nil {
		return models.BitriseConfigMap{}, err
	}

	configName := defaultConfigName()
	bitriseDataMap := models.BitriseConfigMap{
		configName: string(data),
	}

	return bitriseDataMap, nil
}