package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/exp/slog" // nee "log/slog"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	"github.com/lmittmann/tint"
)

const usage = `Usage:
    rockon-validator [--check] [--diff] [--write] [--root FILE] [--verbose|--debug] FILE...

Options:
    -c, --check    Check the FILE(s) for the correct syntax and return non-zero if invalid.
    -d, --diff     Check the FILE(s) for the correct syntax and output a diff if different.
    -w, --write    Check the FILE(s) and write any changes back to disk in-place.

    -r, --root     root.json file used to verify that the rockon is mentioned in said file.
                   Default: same directory as FILE

    -v, --verbose  Enable more logging
    --debug        Enable debug logging
`

var (
	checkFlag, diffFlag, writeFlag, verboseFlag, debugFlag bool
	rootFlag, rootFile                                     string
	logger                                                 *slog.Logger
)

func parseFlags() {
	if len(os.Args) == 1 {
		flag.Usage()
		os.Exit(1)
	}

	flag.BoolVar(&checkFlag, "c", false, "check the file")
	flag.BoolVar(&checkFlag, "check", false, "check the file")
	flag.BoolVar(&diffFlag, "d", false, "diff the file")
	flag.BoolVar(&diffFlag, "diff", false, "diff the file")
	flag.BoolVar(&writeFlag, "w", false, "write the file")
	flag.BoolVar(&writeFlag, "write", false, "write the file")
	flag.StringVar(&rootFlag, "r", "", "root.json file to check")
	flag.StringVar(&rootFlag, "root", "", "root.json file to check")
	flag.BoolVar(&verboseFlag, "v", false, "enable more logging")
	flag.BoolVar(&verboseFlag, "verbose", false, "enable more logging")
	flag.BoolVar(&debugFlag, "debug", false, "enable debug logging")

	flag.Parse()
}

func parseFileArgs() (filePaths []string) {
	for _, f := range flag.Args() {
		glob, _ := filepath.Glob(f)
		filePaths = append(filePaths, glob...)
	}
	return filePaths
}

func setupLogger(logLevel *slog.LevelVar) *slog.Logger {
	logOpts := &tint.Options{
		Level:      logLevel,
		TimeFormat: " ",
	}
	logHandler := tint.NewHandler(os.Stderr, logOpts)
	logger := slog.New(logHandler)
	slog.SetDefault(logger)
	return logger
}

func checkRootMap(rootMap map[string]string, filename string, rockon RockOn) {
	var found bool
	var foundName string
	for k := range rootMap {
		found = rootMap[k] == filename
		if found {
			foundName = k
			break
		}
	}
	for name := range rockon {
		if found {
			if name != foundName {
				slog.Warn("Name does not match", slog.String("root.json", foundName), slog.String("name", name), slog.String("rockon", filepath.Base(filename)))
			}
		} else {
			rootMap[name] = filename
			logger.Debug("root.json map", slog.Any("rootMap", rootMap))
		}
	}
}

func main() {
	logLevel := &slog.LevelVar{}
	logLevel.Set(slog.LevelWarn)
	logger = setupLogger(logLevel)

	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	parseFlags()

	if verboseFlag {
		logLevel.Set(slog.LevelInfo)
	}

	if debugFlag {
		logLevel.Set(slog.LevelDebug)
	}

	logger.Debug("Operation flags", slog.Bool("checkFlag", checkFlag), slog.Bool("diffFlag", diffFlag), slog.Bool("writeFlag", writeFlag))
	logger.Debug("Verbosity flags", slog.Bool("verboseFlag", verboseFlag), slog.Bool("debugFlag", debugFlag))
	logger.Debug("root.json flags", slog.String("rootFlag", rootFlag), slog.String("rootFile", rootFile))
	rootMap := map[string]string{}

	var numDiffFiles int
	for _, f := range parseFileArgs() {
		logger.Info("Checking", slog.String("file", f))
		data, err := os.ReadFile(f)
		if err != nil {
			logger.Error("", slog.Any("err", err))
			os.Exit(1)
		}
		dataString := string(data)

		rootFile = rootFlag
		if rootFlag == "" {
			rootFile = filepath.Join(filepath.Dir(f), "root.json")
		}
		rootData, _ := os.ReadFile(rootFile)
		json.Unmarshal(rootData, &rootMap)
		logger.Debug("root.json flags", slog.String("rootFlag", rootFlag), slog.String("rootFile", rootFile))

		var rockon RockOn
		err = json.Unmarshal(data, &rockon)
		if err != nil {
			// It may be the root.json, so skip it
			err1 := json.Unmarshal(data, &rootMap)
			if err1 == nil {
				logger.Warn("Possible root.json, skipping", slog.String("file", f))
				continue
			}
			logger.Error(f, slog.Any("err", err))
			os.Exit(1)
		}

		checkRootMap(rootMap, filepath.Base(f), rockon)

		result, err := rockon.ToJSON()
		if err != nil {
			logger.Error("", slog.Any("err", err))
			os.Exit(1)
		}

		if dataString != result {
			numDiffFiles++
		}

		if diffFlag {
			aPath := "a/" + strings.TrimPrefix(f, "/")
			bPath := "b/" + strings.TrimPrefix(f, "/")
			edits := myers.ComputeEdits(span.URIFromPath(aPath), dataString, result)
			fmt.Println(gotextdiff.ToUnified(aPath, bPath, dataString, edits))
		}

		if writeFlag {
			stat, _ := os.Stat(f)
			logger.Debug("Writing rockon", slog.String("file", f))
			err = os.WriteFile(f, []byte(result), stat.Mode())
			if err != nil {
				logger.Error("Writing rockon", slog.Any("err", err))
			}
			rootStat, err := os.Stat(rootFile)
			if os.IsNotExist(err) {
				rootStat = stat
			}
			rootJson, _ := json.MarshalIndent(rootMap, "", "    ")
			logger.Debug("Writing root", slog.String("file", rootFile))
			err = os.WriteFile(rootFile, rootJson, rootStat.Mode())
			if err != nil {
				logger.Error("Writing root", slog.Any("err", err))
			}
		}
	}

	if checkFlag {
		os.Exit(numDiffFiles)
	}
}
