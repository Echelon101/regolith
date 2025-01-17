package regolith

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/otiai10/copy"
)

// RecycledSetupTmpFiles set up the workspace for the filters. The function
// uses cached data about the state of the project files to reduce the number
// of file system operations.
func RecycledSetupTmpFiles(config Config, profile Profile, dotRegolithPath string) error {
	start := time.Now()
	tmpPath := filepath.Join(dotRegolithPath, "tmp")
	err := os.MkdirAll(tmpPath, 0755)
	if err != nil {
		return WrapErrorf(err, osMkdirError, tmpPath)
	}
	// Copy the contents of the 'regolith' folder to '[dotRegolith]/tmp'
	if config.ResourceFolder != "" {
		Logger.Debugf("Copying project files to \"%s\"", tmpPath)
		err = FullRecycledMoveOrCopy(
			config.ResourceFolder, filepath.Join(tmpPath, "RP"),
			RecycledMoveOrCopySettings{
				canMove:                 false,
				saveSourceHashes:        false,
				saveTargetHashes:        false,
				copyTargetAclFromParent: false,
				reloadSourceHashes:      true,
			})
		if err != nil {
			return WrapErrorf(
				err, "Failed to setup RP folder in the temporary directory.")
		}
	}
	if config.BehaviorFolder != "" {
		err = FullRecycledMoveOrCopy(
			config.BehaviorFolder, filepath.Join(tmpPath, "BP"),
			RecycledMoveOrCopySettings{
				canMove:                 false,
				saveSourceHashes:        false,
				saveTargetHashes:        false,
				copyTargetAclFromParent: false,
				reloadSourceHashes:      true,
			})
		if err != nil {
			return WrapErrorf(
				err, "Failed to setup BP folder in the temporary directory.")
		}
	}
	if config.DataPath != "" {
		err = FullRecycledMoveOrCopy(
			config.DataPath, filepath.Join(tmpPath, "data"),
			RecycledMoveOrCopySettings{
				canMove:                 false,
				saveSourceHashes:        false,
				saveTargetHashes:        false,
				copyTargetAclFromParent: false,
				reloadSourceHashes:      true,
			})
		if err != nil {
			return WrapErrorf(
				err, "Failed to setup data folder in the temporary directory.")
		}
	}

	Logger.Debug("Setup done in ", time.Since(start))
	return nil
}

// SetupTmpFiles set up the workspace for the filters.
func SetupTmpFiles(config Config, profile Profile, dotRegolithPath string) error {
	start := time.Now()
	// Setup Directories
	tmpPath := filepath.Join(dotRegolithPath, "tmp")
	Logger.Debugf("Cleaning \"%s\"", tmpPath)
	err := os.RemoveAll(tmpPath)
	if err != nil {
		return WrapErrorf(err, osRemoveError, tmpPath)
	}

	err = os.MkdirAll(tmpPath, 0755)
	if err != nil {
		return WrapErrorf(err, osMkdirError, tmpPath)
	}

	// Copy the contents of the 'regolith' folder to '[dotRegolithPath]/tmp'
	Logger.Debugf("Copying project files to \"%s\"", tmpPath)
	// Avoid repetetive code of preparing ResourceFolder, BehaviorFolder
	// and DataPath with a closure
	setup_tmp_directory := func(
		path, shortName, descriptiveName string,
	) error {
		p := filepath.Join(tmpPath, shortName)
		if path != "" {
			stats, err := os.Stat(path)
			if err != nil {
				if os.IsNotExist(err) {
					Logger.Warnf(
						"%s %q does not exist", descriptiveName, path)
					err = os.MkdirAll(p, 0755)
					if err != nil {
						return WrapErrorf(err, osMkdirError, p)
					}
				}
			} else if stats.IsDir() {
				err = copy.Copy(
					path,
					p,
					copy.Options{PreserveTimes: false, Sync: false})
				if err != nil {
					return WrapErrorf(err, osCopyError, path, p)
				}
			} else { // The folder paths leads to a file
				return WrappedErrorf(isDirNotADirError, path)
			}
		} else {
			err = os.MkdirAll(p, 0755)
			if err != nil {
				return WrapErrorf(err, osMkdirError, p)
			}
		}
		return nil
	}

	err = setup_tmp_directory(config.ResourceFolder, "RP", "resource folder")
	if err != nil {
		return WrapErrorf(
			err, "Failed to setup RP folder in the temporary directory.")
	}
	err = setup_tmp_directory(config.BehaviorFolder, "BP", "behavior folder")
	if err != nil {
		return WrapErrorf(
			err, "Failed to setup BP folder in the temporary directory.")
	}
	err = setup_tmp_directory(config.DataPath, "data", "data folder")
	if err != nil {
		return WrapErrorf(
			err, "Failed to setup data folder in the temporary directory.")
	}

	Logger.Debug("Setup done in ", time.Since(start))
	return nil
}

func CheckProfileImpl(
	profile Profile, profileName string, config Config,
	parentContext *RunContext, dotRegolithPath string,
) error {
	// Check whether every filter, uses a supported filter type
	for _, f := range profile.Filters {
		err := f.Check(RunContext{
			Config:          &config,
			Parent:          parentContext,
			Profile:         profileName,
			DotRegolithPath: dotRegolithPath,
		})
		if err != nil {
			return WrapErrorf(err, filterRunnerCheckError, f.GetId())
		}
	}
	return nil
}

// RecycledRunProfile loads the profile from config.json and runs it based on the
// context. If context is in the watch mode, it can repeat the process multiple
// times in case of interruptions (changes in the source files).
func RecycledRunProfile(context RunContext) error {
	// profileName string, profile *Profile, config *Config

	// saveTmp saves the state of the tmp files. This is useful only if runnig
	// in the watch mode.
	saveTmp := func() error {
		err1 := SaveStateInDefaultCache(filepath.Join(context.DotRegolithPath, "tmp/RP"))
		err2 := SaveStateInDefaultCache(filepath.Join(context.DotRegolithPath, "tmp/BP"))
		err3 := SaveStateInDefaultCache(filepath.Join(context.DotRegolithPath, "tmp/data"))
		if err := firstErr(err1, err2, err3); err != nil {
			err1 := ClearCachedStates() // Just to be safe - clear cached states
			if err1 != nil {
				err = WrapError(err1, clearCachedStatesError)
			}
			return WrapError(err, "Failed to save file path states in cache.")
		}
		return nil
	}
	// The label and goto can be easily changed to a loop with continue and
	// break but I find this more readable. If you want to change it, because
	// you believe goto is forbidden, dark art then feel free to do so.
start:
	// Prepare tmp files
	profile, err := context.GetProfile()
	if err != nil {
		return WrapErrorf(err, runContextGetProfileError)
	}
	err = RecycledSetupTmpFiles(*context.Config, profile, context.DotRegolithPath)
	if err != nil {
		err1 := ClearCachedStates() // Just to be safe clear cached states
		if err1 != nil {
			err = WrapError(err1, clearCachedStatesError)
		}
		return WrapErrorf(err, setupTmpFilesError, context.DotRegolithPath)
	}
	if context.IsInterrupted() {
		if err := saveTmp(); err != nil {
			return PassError(err)
		}
		goto start
	}
	// Run the profile
	interrupted, err := WatchProfileImpl(context)
	if err != nil {
		return PassError(err)
	}
	if interrupted { // Save the current target state before rerun
		if err := saveTmp(); err != nil {
			return PassError(err)
		}
		goto start
	}
	// Export files
	Logger.Info("Moving files to target directory.")
	start := time.Now()
	err = RecycledExportProject(
		profile, context.Config.Name, context.Config.DataPath, context.DotRegolithPath)
	if err != nil {
		err1 := ClearCachedStates() // Just to be safe clear cached states
		if err1 != nil {
			err = WrapError(err1, clearCachedStatesError)
		}
		return WrapError(err, exportProjectError)
	}
	if context.IsInterrupted("data") { // Ignore the interruptions from the data path
		if err := saveTmp(); err != nil {
			return PassError(err)
		}
		goto start
	}
	Logger.Debug("Done in ", time.Since(start))
	return nil
}

// RunProfile loads the profile from config.json and runs it based on the
// context. If context is in the watch mode, it can repeat the process multiple
// times in case of interruptions (changes in the source files).
func RunProfile(context RunContext) error {
	// Clear states to not conflict with recycled mode, error handling not
	// important
	ClearCachedStates()
start:
	// Prepare tmp files
	profile, err := context.GetProfile()
	if err != nil {
		return WrapErrorf(err, runContextGetProfileError)
	}
	err = SetupTmpFiles(*context.Config, profile, context.DotRegolithPath)
	if err != nil {
		return WrapErrorf(err, setupTmpFilesError, context.DotRegolithPath)
	}
	if context.IsInterrupted() {
		goto start
	}
	// Run the profile
	interrupted, err := WatchProfileImpl(context)
	if err != nil {
		return PassError(err)
	}
	if interrupted {
		goto start
	}
	// Export files
	Logger.Info("Moving files to target directory.")
	start := time.Now()
	err = ExportProject(
		profile, context.Config.Name, context.Config.DataPath, context.DotRegolithPath)
	if err != nil {
		return WrapError(err, exportProjectError)
	}
	if context.IsInterrupted("data") {
		goto start
	}
	Logger.Debug("Done in ", time.Since(start))
	return nil
}

// WatchProfileImpl runs the profile from the given context and returns true
// if the execution was interrupted.
func WatchProfileImpl(context RunContext) (bool, error) {
	profile, err := context.GetProfile()
	if err != nil {
		return false, WrapErrorf(err, runContextGetProfileError)
	}
	// Run the filters!
	for filter := range profile.Filters {
		filter := profile.Filters[filter]
		// Disabled filters are skipped
		if filter.IsDisabled() {
			Logger.Infof("Filter \"%s\" is disabled, skipping.", filter.GetId())
			continue
		}
		// Skip printing if the filter ID is empty (most likely a nested profile)
		if filter.GetId() != "" {
			Logger.Infof("Running filter %s", filter.GetId())
		}
		// Run the filter in watch mode
		start := time.Now()
		interrupted, err := filter.Run(context)
		Logger.Debugf("Executed in %s", time.Since(start))
		if err != nil {
			err1 := ClearCachedStates() // Just to be safe clear cached states
			if err1 != nil {
				err = WrapError(err1, clearCachedStatesError)
			}
			return false, WrapErrorf(
				err, filterRunnerRunError, filter.GetId())
		}
		if interrupted {
			return true, nil
		}
	}
	return false, nil
}

// subfilterCollection returns a collection of filters from a
// "filter.json" file of a remote filter.
func (f *RemoteFilter) subfilterCollection(dotRegolithPath string) (*FilterCollection, error) {
	path := filepath.Join(f.GetDownloadPath(dotRegolithPath), "filter.json")
	result := &FilterCollection{Filters: []FilterRunner{}}
	file, err := ioutil.ReadFile(path)

	if err != nil {
		return nil, WrappedErrorf( // Don't pass OS error here. It's often confusing
			"Couldn't read filter data from path:\n"+
				"%s\n"+
				"Did you install the filter?\n"+
				"You can install all of the filters by running:\n"+
				"regolith install-all",
			path,
		)
	}

	var filterCollection map[string]interface{}
	err = json.Unmarshal(file, &filterCollection)
	if err != nil {
		return nil, WrapErrorf(err, jsonUnmarshalError, path)
	}
	// Filters
	filtersObj, ok := filterCollection["filters"]
	if !ok {
		return nil, extraFilterJsonErrorInfo(
			path, WrappedErrorf(jsonPathMissingError, "filters"))
	}
	filters, ok := filtersObj.([]interface{})
	if !ok {
		return nil, extraFilterJsonErrorInfo(
			path, WrappedErrorf(jsonPathTypeError, "filters", "array"))
	}
	for i, filter := range filters {
		filter, ok := filter.(map[string]interface{})
		jsonPath := fmt.Sprintf("filters->%d", i) // Used for error messages
		if !ok {
			return nil, extraFilterJsonErrorInfo(
				path, WrappedErrorf(jsonPathTypeError, jsonPath, "object"))
		}
		// Using the same JSON data to create both the filter
		// definiton (installer) and the filter (runner)
		filterId := fmt.Sprintf("%v:subfilter%v", f.Id, i)
		filterInstaller, err := FilterInstallerFromObject(filterId, filter)
		if err != nil {
			return nil, extraFilterJsonErrorInfo(
				path, WrapErrorf(err, jsonPathParseError, jsonPath))
		}
		// Remote filters don't have the "filter" key but this would break the
		// code as it's required by local filters. Adding it here to make the
		// code work.
		// TODO - this is a hack, fix it!
		filter["filter"] = filterId
		filterRunner, err := filterInstaller.CreateFilterRunner(filter)
		if err != nil {
			// TODO - better filterName?
			filterName := fmt.Sprintf("%v filter from %s.", nth(i), path)
			return nil, WrapErrorf(
				err, createFilterRunnerError, filterName)
		}
		if _, ok := filterRunner.(*RemoteFilter); ok {
			// TODO - we could possibly implement recursive filters here
			return nil, WrappedErrorf(
				"Regolith detected a reference to a remote filter inside "+
					"another remote filter.\n"+
					"This feature is not supported.\n"+
					"Filter name: %s"+
					"Filter configuration file: %s\n"+
					"JSON path to remote filter reference: filters->%d",
				f.Id, path, i)
		}
		filterRunner.CopyArguments(f)
		result.Filters = append(result.Filters, filterRunner)
	}
	return result, nil
}

// FilterCollection is a list of filters
type FilterCollection struct {
	Filters []FilterRunner `json:"filters"`
}

type Profile struct {
	FilterCollection
	ExportTarget ExportTarget `json:"export,omitempty"`
}

func ProfileFromObject(
	obj map[string]interface{}, filterDefinitions map[string]FilterInstaller,
) (Profile, error) {
	result := Profile{}
	// Filters
	if _, ok := obj["filters"]; !ok {
		return result, WrappedErrorf(jsonPathMissingError, "filters")
	}
	filters, ok := obj["filters"].([]interface{})
	if !ok {
		return result, WrappedErrorf(jsonPathTypeError, "filters", "array")
	}
	for i, filter := range filters {
		filter, ok := filter.(map[string]interface{})
		if !ok {
			return result, WrappedErrorf(
				jsonPathTypeError, fmt.Sprintf("filters->%d", i), "object")
		}
		filterRunner, err := FilterRunnerFromObjectAndDefinitions(
			filter, filterDefinitions)
		if err != nil {
			return result, WrapErrorf(
				err, jsonPathParseError, fmt.Sprintf("filters->%d", i))
		}
		result.Filters = append(result.Filters, filterRunner)
	}
	// ExportTarget
	if _, ok := obj["export"]; !ok {
		return result, WrappedErrorf(jsonPathMissingError, "export")
	}
	export, ok := obj["export"].(map[string]interface{})
	if !ok {
		return result, WrappedErrorf(jsonPathTypeError, "export", "object")
	}
	exportTarget, err := ExportTargetFromObject(export)
	if err != nil {
		return result, WrapErrorf(err, jsonPathParseError, "export")
	}
	result.ExportTarget = exportTarget
	return result, nil
}
