package main

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Format string

const (
	FormatPlain Format = "plain"
	FormatJSON  Format = "json"
)

type OutputKind string

const (
	OutputStdout OutputKind = "stdout"
	OutputDir    OutputKind = "dir"
	OutputHTTP   OutputKind = "http"
)

type HTTPAuthMode string

const (
	HTTPAuthNone   HTTPAuthMode = "none"
	HTTPAuthBearer HTTPAuthMode = "bearer"
	HTTPAuthBasic  HTTPAuthMode = "basic"
)

type HTTPFormat string

const (
	HTTPFormatJSONLine HTTPFormat = "jsonline"
	HTTPFormatGELF     HTTPFormat = "gelf"
)

type OverflowPolicy string

const (
	OverflowDropOldest OverflowPolicy = "drop-oldest"
	OverflowDropNewest OverflowPolicy = "drop-newest"
	OverflowBlock      OverflowPolicy = "block"
)

type SourceKind string

const (
	SourceExec   SourceKind = "exec"
	SourceStdin  SourceKind = "stdin"
	SourceFile   SourceKind = "file"
	SourceListen SourceKind = "listen"
)

type FileStart string

const (
	FileStartEnd       FileStart = "end"
	FileStartBeginning FileStart = "beginning"
)

type InputKind string

const (
	InputKindFile    InputKind = "file"
	InputKindJournal InputKind = "journal"
	InputKindTCP     InputKind = "tcp"
	InputKindUDP     InputKind = "udp"
)

type JournalStart string

const (
	JournalStartEnd JournalStart = "end"
	JournalStartAll JournalStart = "all"
)

type Config struct {
	Output               OutputKind
	OutputTarget         string
	Outputs              []OutputConfig
	Format               Format
	ANSI                 ansiMode
	Version              bool
	MetadataFields       []MetadataField
	CustomMetadata       map[string]any
	CustomMetadataFile   string
	DirMaxBytes          int
	DirMaxFiles          int
	HTTPBatchRecords     int
	HTTPBatchRecordsSet  bool
	HTTPBatchBytes       int
	HTTPBatchBytesSet    bool
	HTTPTimeout          time.Duration
	HTTPRetries          int
	HTTPAuth             HTTPAuthMode
	HTTPAuthSecretFile   string
	HTTPFormat           HTTPFormat
	QueueRecords         int
	QueueBytes           int
	Overflow             OverflowPolicy
	MaxFragmentBytes     int
	PartialFlushInterval time.Duration
	FlushInterval        time.Duration
	PostExitDrainTimeout time.Duration
	TerminationTimeout   time.Duration
	DebugCPUProfile      string
	DebugMemProfile      string
	ConfigPath           string
	ConfigMode           bool
	Source               SourceKind
	FilePath             string
	SourceName           string
	ListenNetwork        string
	ListenAddress        string
	ListenMaxConnections int
	HealthListen         string
	FileStart            FileStart
	FilePollInterval     time.Duration
	Inputs               []InputConfig
	Command              []string
}

type InputConfig struct {
	Kind                 InputKind
	Path                 string
	Listen               string
	MaxConnections       int
	MaxConnectionsSet    bool
	Directory            string
	JournalStart         JournalStart
	CursorFile           string
	Source               string
	ANSI                 ansiMode
	ANSISet              bool
	QueueRecords         int
	QueueRecordsSet      bool
	QueueBytes           int
	QueueBytesSet        bool
	Overflow             OverflowPolicy
	OverflowSet          bool
	MaxFragmentBytes     int
	MaxFragmentBytesSet  bool
	PartialFlushInterval time.Duration
	PartialFlushSet      bool
	FileStart            FileStart
	FileStartSet         bool
	FilePollInterval     time.Duration
	FilePollIntervalSet  bool
}

type OutputConfig struct {
	Kind                OutputKind
	Target              string
	DirMaxBytes         int
	DirMaxFiles         int
	DirMaxFilesSet      bool
	HTTPBatchRecords    int
	HTTPBatchRecordsSet bool
	HTTPBatchBytes      int
	HTTPBatchBytesSet   bool
	HTTPTimeout         time.Duration
	HTTPRetries         int
	HTTPRetriesSet      bool
	HTTPAuth            HTTPAuthMode
	HTTPAuthSecretFile  string
	HTTPFormat          HTTPFormat
}

type outputFlag []string

func (f *outputFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func (f *outputFlag) String() string {
	return strings.Join(*f, ",")
}

type stringListFlag []string

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

type intSetFlag struct {
	value *int
	set   *bool
}

func (f intSetFlag) Set(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	*f.value = n
	*f.set = true
	return nil
}

func (f intSetFlag) String() string {
	if f.value == nil {
		return ""
	}
	return strconv.Itoa(*f.value)
}

type bytesSetFlag struct {
	value *int
	set   *bool
}

func (f bytesSetFlag) Set(value string) error {
	n, err := parseBytes(value)
	if err != nil {
		return err
	}
	*f.value = n
	*f.set = true
	return nil
}

func (f bytesSetFlag) String() string {
	if f.value == nil {
		return ""
	}
	return strconv.Itoa(*f.value)
}

func DefaultConfig() Config {
	return Config{
		Output:               OutputStdout,
		Format:               FormatPlain,
		ANSI:                 ANSIStrip,
		DirMaxBytes:          1_000_000,
		DirMaxFiles:          10,
		HTTPBatchRecords:     100,
		HTTPBatchBytes:       4 * 1024 * 1024,
		HTTPTimeout:          5 * time.Second,
		HTTPRetries:          2,
		HTTPAuth:             HTTPAuthNone,
		HTTPFormat:           HTTPFormatJSONLine,
		QueueRecords:         64 * 1024,
		QueueBytes:           64 * 1024 * 1024,
		Overflow:             OverflowBlock,
		MaxFragmentBytes:     64 * 1024,
		PartialFlushInterval: time.Second,
		FlushInterval:        100 * time.Millisecond,
		PostExitDrainTimeout: 5 * time.Second,
		TerminationTimeout:   5 * time.Second,
		Source:               SourceExec,
		ListenMaxConnections: 64,
		FileStart:            FileStartEnd,
		FilePollInterval:     250 * time.Millisecond,
	}
}

func ParseConfig(args []string) (Config, error) {
	cfg := DefaultConfig()
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return cfg, ErrHelp
		}
		if arg == "--version" {
			cfg.Version = true
			return cfg, nil
		}
	}
	configPath, hasConfig, configErr := parseConfigPath(args)
	if configErr != nil {
		return cfg, configErr
	}
	if hasConfig {
		cfg.DebugCPUProfile, cfg.DebugMemProfile, cfg.HealthListen, configErr = parseConfigModeDebugFlags(args)
		if configErr != nil {
			return cfg, configErr
		}
		fileCfg, configErr := ParseConfigFile(configPath)
		if configErr != nil {
			return cfg, configErr
		}
		fileCfg.ConfigPath = configPath
		fileCfg.ConfigMode = true
		fileCfg.DebugCPUProfile = cfg.DebugCPUProfile
		fileCfg.DebugMemProfile = cfg.DebugMemProfile
		if cfg.HealthListen != "" {
			fileCfg.HealthListen = cfg.HealthListen
		}
		return fileCfg, nil
	}
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == -1 {
		sep = len(args)
	} else if sep == len(args)-1 {
		return cfg, errors.New("no command specified after --")
	}

	fs := flag.NewFlagSet("logsurge", flag.ContinueOnError)
	fs.SetOutput(discardWriter{})
	var format string
	var outputFormat string
	var queueBytes string
	var queueSize int
	var maxFragmentBytes string
	var partialFlush string
	var flushInterval string
	var postExitDrain string
	var terminationTimeout string
	var metadataFields string
	var customMetadataFields stringListFlag
	var ansi string
	var dirMaxBytes string
	var httpTimeout string
	var httpAuth string
	var httpFormat string
	var overflow string
	var outputs outputFlag
	var fileStart string
	var filePollInterval string
	var listen string
	fs.StringVar(&cfg.ConfigPath, "config", cfg.ConfigPath, "configuration file path")
	fs.StringVar(&cfg.ConfigPath, "c", cfg.ConfigPath, "alias for --config")
	fs.Var(&outputs, "output", "output sink: stdout, dir, http, or kind=target")
	fs.Var(&outputs, "o", "alias for --output")
	fs.StringVar(&cfg.DebugCPUProfile, "debug-cpuprofile", cfg.DebugCPUProfile, "hidden CPU profile path")
	fs.StringVar(&cfg.DebugMemProfile, "debug-memprofile", cfg.DebugMemProfile, "hidden memory profile path")
	fs.StringVar(&cfg.FilePath, "file", cfg.FilePath, "input source file to follow")
	fs.StringVar(&cfg.FilePath, "f", cfg.FilePath, "alias for --file")
	fs.StringVar(&listen, "listen", "", "loopback network input: tcp://HOST:PORT or udp://HOST:PORT")
	fs.IntVar(&cfg.ListenMaxConnections, "listen-max-connections", cfg.ListenMaxConnections, "maximum active TCP input connections")
	fs.StringVar(&cfg.HealthListen, "health-listen", cfg.HealthListen, "loopback health/metrics listener")
	fs.StringVar(&fileStart, "file-start", string(cfg.FileStart), "file input start position: beginning, end")
	fs.StringVar(&filePollInterval, "file-poll-interval", cfg.FilePollInterval.String(), "file input poll interval")
	fs.StringVar(&format, "format", string(cfg.Format), "output format")
	fs.StringVar(&format, "F", string(cfg.Format), "alias for --format")
	fs.StringVar(&outputFormat, "output-format", "", "output format")
	fs.StringVar(&ansi, "ansi", string(cfg.ANSI), "ANSI escape handling: strip, keep")
	fs.IntVar(&cfg.QueueRecords, "queue-records", cfg.QueueRecords, "maximum queued records")
	fs.IntVar(&queueSize, "queue-size", 0, "alias for --queue-records")
	fs.StringVar(&queueBytes, "queue-bytes", strconv.Itoa(cfg.QueueBytes), "maximum queued bytes")
	fs.StringVar(&overflow, "overflow", string(cfg.Overflow), "overflow policy")
	fs.StringVar(&maxFragmentBytes, "max-fragment-bytes", strconv.Itoa(cfg.MaxFragmentBytes), "maximum fragment bytes")
	fs.StringVar(&partialFlush, "partial-flush-interval", cfg.PartialFlushInterval.String(), "partial flush interval")
	fs.StringVar(&flushInterval, "flush-interval", cfg.FlushInterval.String(), "writer flush interval")
	fs.StringVar(&postExitDrain, "post-exit-drain-timeout", cfg.PostExitDrainTimeout.String(), "post-exit drain timeout")
	fs.StringVar(&terminationTimeout, "termination-timeout", cfg.TerminationTimeout.String(), "TERM-to-KILL timeout after sink failure")
	fs.StringVar(&metadataFields, "metadata", "", "JSON metadata fields: hostname,source,line_end,continued")
	fs.StringVar(&metadataFields, "m", "", "alias for --metadata")
	fs.Var(&customMetadataFields, "metadata-field", "custom metadata key=value; repeatable")
	fs.StringVar(&cfg.CustomMetadataFile, "metadata-file", cfg.CustomMetadataFile, "flat JSON object with custom metadata")
	fs.StringVar(&dirMaxBytes, "dir-max-bytes", strconv.Itoa(cfg.DirMaxBytes), "directory sink rotation size")
	fs.IntVar(&cfg.DirMaxFiles, "dir-max-files", cfg.DirMaxFiles, "directory sink retained rotated files; 0 disables retention")
	fs.Var(intSetFlag{value: &cfg.HTTPBatchRecords, set: &cfg.HTTPBatchRecordsSet}, "http-batch-records", "HTTP sink records per batch")
	fs.Var(bytesSetFlag{value: &cfg.HTTPBatchBytes, set: &cfg.HTTPBatchBytesSet}, "http-batch-bytes", "HTTP sink encoded bytes per batch")
	fs.StringVar(&httpTimeout, "http-timeout", cfg.HTTPTimeout.String(), "HTTP sink request timeout")
	fs.IntVar(&cfg.HTTPRetries, "http-retries", cfg.HTTPRetries, "HTTP sink retries per batch")
	fs.StringVar(&httpAuth, "http-auth", string(cfg.HTTPAuth), "HTTP auth mode: none, bearer, basic")
	fs.StringVar(&httpFormat, "http-format", string(cfg.HTTPFormat), "HTTP wire format: jsonline, gelf")
	fs.StringVar(&cfg.HTTPAuthSecretFile, "http-auth-secret-file", cfg.HTTPAuthSecretFile, "HTTP auth secret file")
	if err := fs.Parse(args[:sep]); err != nil {
		return cfg, err
	}
	if cfg.ConfigPath != "" {
		return cfg, errors.New("daemon mode must not be combined with ad-hoc mode options")
	}
	overflowSet := hasFlag(args[:sep], "--overflow")
	if queueSize > 0 {
		cfg.QueueRecords = queueSize
	}
	chosenFormat := format
	if outputFormat != "" {
		chosenFormat = outputFormat
	}
	switch Format(chosenFormat) {
	case FormatPlain, FormatJSON:
		cfg.Format = Format(chosenFormat)
	default:
		return cfg, fmt.Errorf("unsupported format %q", chosenFormat)
	}
	switch OverflowPolicy(overflow) {
	case OverflowDropOldest, OverflowDropNewest, OverflowBlock:
		cfg.Overflow = OverflowPolicy(overflow)
	default:
		return cfg, fmt.Errorf("unsupported overflow policy %q", overflow)
	}
	switch HTTPAuthMode(httpAuth) {
	case HTTPAuthNone, HTTPAuthBearer, HTTPAuthBasic:
		cfg.HTTPAuth = HTTPAuthMode(httpAuth)
	default:
		return cfg, fmt.Errorf("unsupported HTTP auth mode %q", httpAuth)
	}
	switch HTTPFormat(httpFormat) {
	case HTTPFormatJSONLine, HTTPFormatGELF:
		cfg.HTTPFormat = HTTPFormat(httpFormat)
	default:
		return cfg, fmt.Errorf("unsupported HTTP format %q", httpFormat)
	}
	switch ansiMode(ansi) {
	case ANSIStrip, ANSIKeep:
		cfg.ANSI = ansiMode(ansi)
	default:
		return cfg, fmt.Errorf("unsupported ANSI mode %q", ansi)
	}
	var err error
	if cfg.QueueBytes, err = parseBytes(queueBytes); err != nil {
		return cfg, fmt.Errorf("invalid --queue-bytes: %w", err)
	}
	if cfg.MaxFragmentBytes, err = parseBytes(maxFragmentBytes); err != nil {
		return cfg, fmt.Errorf("invalid --max-fragment-bytes: %w", err)
	}
	if cfg.PartialFlushInterval, err = time.ParseDuration(partialFlush); err != nil {
		return cfg, fmt.Errorf("invalid --partial-flush-interval: %w", err)
	}
	if cfg.FlushInterval, err = time.ParseDuration(flushInterval); err != nil {
		return cfg, fmt.Errorf("invalid --flush-interval: %w", err)
	}
	if cfg.PostExitDrainTimeout, err = time.ParseDuration(postExitDrain); err != nil {
		return cfg, fmt.Errorf("invalid --post-exit-drain-timeout: %w", err)
	}
	if cfg.TerminationTimeout, err = time.ParseDuration(terminationTimeout); err != nil {
		return cfg, fmt.Errorf("invalid --termination-timeout: %w", err)
	}
	if cfg.MetadataFields, err = ParseMetadataFields(metadataFields); err != nil {
		return cfg, fmt.Errorf("invalid --metadata: %w", err)
	}
	if cfg.CustomMetadata, err = loadCustomMetadata(cfg.CustomMetadataFile, []string(customMetadataFields)); err != nil {
		return cfg, fmt.Errorf("invalid custom metadata: %w", err)
	}
	if cfg.DirMaxBytes, err = parseBytes(dirMaxBytes); err != nil {
		return cfg, fmt.Errorf("invalid --dir-max-bytes: %w", err)
	}
	if cfg.HTTPTimeout, err = time.ParseDuration(httpTimeout); err != nil {
		return cfg, fmt.Errorf("invalid --http-timeout: %w", err)
	}
	switch FileStart(fileStart) {
	case FileStartBeginning, FileStartEnd:
		cfg.FileStart = FileStart(fileStart)
	default:
		return cfg, fmt.Errorf("unsupported file start %q", fileStart)
	}
	if cfg.FilePollInterval, err = time.ParseDuration(filePollInterval); err != nil {
		return cfg, fmt.Errorf("invalid --file-poll-interval: %w", err)
	}
	if cfg.QueueRecords <= 0 {
		return cfg, errors.New("--queue-records must be greater than zero")
	}
	if cfg.QueueBytes <= 0 {
		return cfg, errors.New("--queue-bytes must be greater than zero")
	}
	if cfg.MaxFragmentBytes <= 0 {
		return cfg, errors.New("--max-fragment-bytes must be greater than zero")
	}
	if cfg.MaxFragmentBytes > cfg.QueueBytes {
		return cfg, errors.New("--max-fragment-bytes must not exceed --queue-bytes")
	}
	if cfg.FlushInterval < 0 || cfg.PartialFlushInterval < 0 || cfg.PostExitDrainTimeout < 0 || cfg.TerminationTimeout < 0 || cfg.FilePollInterval < 0 {
		return cfg, errors.New("durations must not be negative")
	}
	if cfg.FilePollInterval == 0 {
		return cfg, errors.New("--file-poll-interval must be greater than zero")
	}
	if cfg.DirMaxFiles < 0 {
		return cfg, errors.New("--dir-max-files must not be negative")
	}
	if cfg.HTTPBatchRecords <= 0 {
		return cfg, errors.New("--http-batch-records must be greater than zero")
	}
	if cfg.HTTPBatchBytes <= 0 {
		return cfg, errors.New("--http-batch-bytes must be greater than zero")
	}
	if cfg.HTTPRetries < 0 {
		return cfg, errors.New("--http-retries must not be negative")
	}
	if cfg.ListenMaxConnections <= 0 {
		return cfg, errors.New("--listen-max-connections must be greater than zero")
	}
	if cfg.HealthListen != "" {
		if err := validateLoopbackAddress(cfg.HealthListen); err != nil {
			return cfg, fmt.Errorf("invalid --health-listen: %w", err)
		}
	}
	if err := applyOutputFlags(&cfg, []string(outputs)); err != nil {
		return cfg, err
	}
	if err := validateOutput(cfg); err != nil {
		return cfg, err
	}
	if listen != "" {
		if cfg.FilePath != "" {
			return cfg, errors.New("--listen cannot be combined with --file")
		}
		if sep < len(args) {
			return cfg, errors.New("--listen cannot be combined with command separator --")
		}
		if len(fs.Args()) > 0 {
			return cfg, errors.New("--listen cannot be combined with positional arguments")
		}
		network, address, err := parseListenSpec(listen)
		if err != nil {
			return cfg, fmt.Errorf("invalid --listen: %w", err)
		}
		cfg.Source = SourceListen
		cfg.ListenNetwork = network
		cfg.ListenAddress = address
		if !overflowSet {
			cfg.Overflow = OverflowDropOldest
		}
		return cfg, nil
	}
	if cfg.FilePath != "" {
		if sep < len(args) {
			return cfg, errors.New("--file cannot be combined with command separator --")
		}
		if len(fs.Args()) > 0 {
			return cfg, errors.New("--file cannot be combined with positional arguments")
		}
		cfg.Source = SourceFile
		return cfg, nil
	}
	if sep < len(args) {
		cfg.Source = SourceExec
		cfg.Command = append([]string(nil), args[sep+1:]...)
		return cfg, nil
	}
	if len(fs.Args()) > 0 {
		return cfg, errors.New("unexpected arguments without command separator --")
	}
	cfg.Source = SourceStdin
	return cfg, nil
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func validateOutput(cfg Config) error {
	outputs := normalizedOutputs(cfg)
	if len(outputs) == 0 {
		return errors.New("at least one output is required")
	}
	seen := map[OutputKind]bool{}
	hasHTTP := false
	for _, output := range outputs {
		if seen[output.Kind] {
			return fmt.Errorf("duplicate output %q", output.Kind)
		}
		seen[output.Kind] = true
		switch output.Kind {
		case OutputStdout:
			if output.Target != "" {
				return errors.New("stdout output does not accept a target")
			}
		case OutputDir:
			if output.Target == "" {
				return errors.New("dir output requires --output dir=PATH")
			}
		case OutputHTTP:
			hasHTTP = true
			if output.Target == "" {
				return errors.New("http output requires --output http=URL")
			}
			if output.HTTPBatchRecords <= 0 {
				return errors.New("--http-batch-records must be greater than zero")
			}
			if output.HTTPBatchBytes <= 0 {
				return errors.New("--http-batch-bytes must be greater than zero")
			}
			if !strings.HasPrefix(output.Target, "http://") && !strings.HasPrefix(output.Target, "https://") {
				return errors.New("http output URL must start with http:// or https://")
			}
			switch output.HTTPAuth {
			case HTTPAuthNone:
				if output.HTTPAuthSecretFile != "" {
					return errors.New("--http-auth-secret-file requires --http-auth bearer or basic")
				}
			case HTTPAuthBearer:
			case HTTPAuthBasic:
			default:
				return fmt.Errorf("unsupported HTTP auth mode %q", output.HTTPAuth)
			}
			switch output.HTTPFormat {
			case HTTPFormatJSONLine, HTTPFormatGELF:
			default:
				return fmt.Errorf("unsupported HTTP format %q", output.HTTPFormat)
			}
		default:
			return fmt.Errorf("unsupported output %q", output.Kind)
		}
	}
	if !hasHTTP && (cfg.HTTPAuth != HTTPAuthNone || cfg.HTTPAuthSecretFile != "") {
		return errors.New("HTTP auth is only valid with --output http")
	}
	return nil
}

func applyOutputFlags(cfg *Config, values []string) error {
	if len(values) == 0 {
		return finalizeOutputs(cfg)
	}
	outputs := make([]OutputConfig, 0, len(values))
	for _, value := range values {
		output, err := parseOutputSpec(value)
		if err != nil {
			return err
		}
		outputs = append(outputs, output)
	}
	cfg.Outputs = outputs
	return finalizeOutputs(cfg)
}

func parseOutputSpec(value string) (OutputConfig, error) {
	kindText, target, hasTarget := strings.Cut(value, "=")
	if kindText == "" {
		return OutputConfig{}, errors.New("empty output kind")
	}
	if hasTarget && target == "" {
		return OutputConfig{}, fmt.Errorf("missing target for output %q", kindText)
	}
	output := OutputConfig{Kind: OutputKind(kindText), Target: target}
	switch output.Kind {
	case OutputStdout, OutputDir, OutputHTTP:
		return output, nil
	default:
		return OutputConfig{}, fmt.Errorf("unsupported output %q", kindText)
	}
}

func finalizeOutputs(cfg *Config) error {
	outputs := normalizedOutputs(*cfg)
	if len(outputs) == 0 {
		return errors.New("at least one output is required")
	}
	cfg.Outputs = outputs
	cfg.Output = outputs[0].Kind
	cfg.OutputTarget = outputs[0].Target
	return nil
}

func normalizedOutputs(cfg Config) []OutputConfig {
	var outputs []OutputConfig
	if len(cfg.Outputs) > 0 {
		outputs = append([]OutputConfig(nil), cfg.Outputs...)
	} else {
		outputs = []OutputConfig{{Kind: cfg.Output, Target: cfg.OutputTarget}}
	}
	for i := range outputs {
		outputs[i] = fillOutputDefaults(cfg, outputs[i])
	}
	return outputs
}

func fillOutputDefaults(cfg Config, output OutputConfig) OutputConfig {
	if output.DirMaxBytes == 0 {
		output.DirMaxBytes = cfg.DirMaxBytes
	}
	if !output.DirMaxFilesSet {
		output.DirMaxFiles = cfg.DirMaxFiles
	}
	if output.HTTPFormat == "" {
		output.HTTPFormat = cfg.HTTPFormat
	}
	if output.HTTPBatchRecords == 0 {
		if output.Kind == OutputHTTP && output.HTTPFormat == HTTPFormatGELF && !output.HTTPBatchRecordsSet && !cfg.HTTPBatchRecordsSet {
			output.HTTPBatchRecords = 1
		} else {
			output.HTTPBatchRecords = cfg.HTTPBatchRecords
		}
	}
	if output.HTTPBatchBytes == 0 {
		output.HTTPBatchBytes = cfg.HTTPBatchBytes
	}
	if output.HTTPTimeout == 0 {
		output.HTTPTimeout = cfg.HTTPTimeout
	}
	if output.HTTPAuth == "" {
		output.HTTPAuth = cfg.HTTPAuth
	}
	if !output.HTTPRetriesSet {
		output.HTTPRetries = cfg.HTTPRetries
	}
	if output.HTTPAuthSecretFile == "" {
		output.HTTPAuthSecretFile = cfg.HTTPAuthSecretFile
	}
	return output
}

func parseConfigPath(args []string) (string, bool, error) {
	for i, arg := range args {
		if arg == "--" {
			return "", false, nil
		}
		if arg == "--config" {
			if i+1 >= len(args) {
				return "", false, errors.New("missing value for --config")
			}
			return args[i+1], true, nil
		}
		if arg == "-c" {
			if i+1 >= len(args) {
				return "", false, errors.New("missing value for -c")
			}
			return args[i+1], true, nil
		}
		if strings.HasPrefix(arg, "--config=") {
			value := strings.TrimPrefix(arg, "--config=")
			if value == "" {
				return "", false, errors.New("missing value for --config")
			}
			return value, true, nil
		}
		if strings.HasPrefix(arg, "-c=") {
			value := strings.TrimPrefix(arg, "-c=")
			if value == "" {
				return "", false, errors.New("missing value for -c")
			}
			return value, true, nil
		}
	}
	return "", false, nil
}

func parseConfigModeDebugFlags(args []string) (string, string, string, error) {
	fs := flag.NewFlagSet("logsurge", flag.ContinueOnError)
	fs.SetOutput(discardWriter{})
	var configPath string
	var cpuProfile string
	var memProfile string
	var healthListen string
	fs.StringVar(&configPath, "config", "", "configuration file path")
	fs.StringVar(&configPath, "c", "", "alias for --config")
	fs.StringVar(&cpuProfile, "debug-cpuprofile", "", "hidden CPU profile path")
	fs.StringVar(&memProfile, "debug-memprofile", "", "hidden memory profile path")
	fs.StringVar(&healthListen, "health-listen", "", "loopback health/metrics listener")
	if err := fs.Parse(args); err != nil {
		return "", "", "", err
	}
	if len(fs.Args()) > 0 {
		return "", "", "", errors.New("daemon mode cannot be combined with positional arguments or command separator --")
	}
	if healthListen != "" {
		if err := validateLoopbackAddress(healthListen); err != nil {
			return "", "", "", fmt.Errorf("invalid --health-listen: %w", err)
		}
	}
	return cpuProfile, memProfile, healthListen, nil
}

func parseBytes(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty size")
	}
	mult := int64(1)
	last := s[len(s)-1]
	switch last {
	case 'k', 'K':
		mult = 1024
		s = s[:len(s)-1]
	case 'm', 'M':
		mult = 1024 * 1024
		s = s[:len(s)-1]
	case 'g', 'G':
		mult = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 0)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("size must be greater than zero")
	}
	v := n * mult
	if int64(int(v)) != v {
		return 0, errors.New("size too large")
	}
	return int(v), nil
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

var ErrHelp = errors.New("help requested")

const Version = "0.1.0"

func Usage() string {
	defaults := DefaultConfig()
	return fmt.Sprintf(`usage:
  logsurge [options] -- command [args...]

Runs a command, merges stdout/stderr, and writes timestamped log records.

options:
  -o, --output KIND[=TARGET]              output sink; repeatable for distinct kinds (default stdout)
  -c, --config PATH                       run daemon mode from config file
  -f, --file PATH                         follow PATH as the input source instead of stdin or command
  --listen tcp://HOST:PORT|udp://HOST:PORT loopback network input
  --listen-max-connections N              max active TCP input connections (default %d)
  --health-listen HOST:PORT               loopback health and Prometheus metrics endpoint
  --file-start beginning|end              where to start an existing file (default %s)
  --file-poll-interval DURATION           file follow polling interval (default %s)
  -F, --format plain|json                 output format (default %s)
  --output-format plain|json              alias for --format
  --ansi strip|keep                       ANSI escape handling (default %s)
  --queue-records N                       max queued records (default %d)
  --queue-size N                          alias for --queue-records
  --queue-bytes BYTES                     max queued bytes (default %d)
  --overflow drop-oldest|drop-newest|block overflow policy (default %s)
  --max-fragment-bytes BYTES              max fragment size (default %d)
  --partial-flush-interval DURATION       flush unterminated bytes after idle interval (default %s)
  --flush-interval DURATION               writer flush interval (default %s)
  --post-exit-drain-timeout DURATION      drain inherited output after child exits (default %s)
  --termination-timeout DURATION          TERM-to-KILL timeout after sink failure (default %s)
  -m, --metadata FIELDS                   add JSON metadata fields: hostname,source,line_end,continued
  --metadata-field KEY=VALUE              add static custom metadata; repeatable
  --metadata-file PATH                    read static custom metadata from flat JSON object
  --dir-max-bytes BYTES                   directory rotation size (default %d)
  --dir-max-files N                       retained rotated directory files; 0 disables retention (default %d)
  --http-batch-records N                  max records per HTTP POST (default %d)
  --http-batch-bytes BYTES                max encoded bytes per HTTP POST (default %d)
  --http-timeout DURATION                 HTTP request timeout (default %s)
  --http-retries N                        HTTP retries per batch (default %d)
  --http-format jsonline|gelf             HTTP wire format (default %s)
  --http-auth none|bearer|basic           HTTP auth mode (default %s)
  --http-auth-secret-file PATH            secret file; otherwise %s is used
  --version                               print version
  -h, --help                              print help

sizes accept optional K, M, or G suffixes. Durations use Go syntax, e.g. 100ms, 1s.

examples:
  logsurge -- make test
  tail -f app.log | logsurge
  logsurge --file /var/log/app.log
  logsurge --listen tcp://127.0.0.1:5514
  logsurge --format json -- terraform plan
  logsurge --overflow block -- ./important-job
  logsurge --format json --metadata hostname,source,line_end -- ./job
  logsurge --output dir=/var/log/myjob -- ./job
  logsurge --output stdout --output http=http://127.0.0.1:8080/logs -- ./job
  logsurge --format json --output http=http://127.0.0.1:8080/logs -- ./job
`, defaults.ListenMaxConnections, defaults.FileStart, defaults.FilePollInterval, defaults.Format, defaults.ANSI, defaults.QueueRecords, defaults.QueueBytes, defaults.Overflow, defaults.MaxFragmentBytes, defaults.PartialFlushInterval, defaults.FlushInterval, defaults.PostExitDrainTimeout, defaults.TerminationTimeout, defaults.DirMaxBytes, defaults.DirMaxFiles, defaults.HTTPBatchRecords, defaults.HTTPBatchBytes, defaults.HTTPTimeout, defaults.HTTPRetries, defaults.HTTPFormat, defaults.HTTPAuth, httpAuthSecretEnv)
}
