// Textual user interface parts of the config system

package config

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/driveletter"
	"github.com/rclone/rclone/fs/fspath"
	"github.com/rclone/rclone/lib/terminal"
	"golang.org/x/text/unicode/norm"
)

// ReadLine reads some input
var ReadLine = func() string {
	buf := bufio.NewReader(os.Stdin)
	line, err := buf.ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read line: %v", err)
	}
	return strings.TrimSpace(line)
}

// ReadNonEmptyLine prints prompt and calls Readline until non empty
func ReadNonEmptyLine(prompt string) string {
	result := ""
	for result == "" {
		fmt.Print(prompt)
		result = strings.TrimSpace(ReadLine())
	}
	return result
}

// CommandDefault - choose one.  If return is pressed then it will
// chose the defaultIndex if it is >= 0
func CommandDefault(commands []string, defaultIndex int) byte {
	opts := []string{}
	for i, text := range commands {
		def := ""
		if i == defaultIndex {
			def = " (default)"
		}
		fmt.Printf("%c) %s%s\n", text[0], text[1:], def)
		opts = append(opts, text[:1])
	}
	optString := strings.Join(opts, "")
	optHelp := strings.Join(opts, "/")
	for {
		fmt.Printf("%s> ", optHelp)
		result := strings.ToLower(ReadLine())
		if len(result) == 0 && defaultIndex >= 0 {
			return optString[defaultIndex]
		}
		if len(result) != 1 {
			continue
		}
		i := strings.Index(optString, string(result[0]))
		if i >= 0 {
			return result[0]
		}
	}
}

// Command - choose one
func Command(commands []string) byte {
	return CommandDefault(commands, -1)
}

// Confirm asks the user for Yes or No and returns true or false
//
// If the user presses enter then the Default will be used
func Confirm(Default bool) bool {
	defaultIndex := 0
	if !Default {
		defaultIndex = 1
	}
	return CommandDefault([]string{"yYes", "nNo"}, defaultIndex) == 'y'
}

// Choose one of the defaults or type a new string if newOk is set
func Choose(what string, defaults, help []string, newOk bool) string {
	valueDescription := "an existing"
	if newOk {
		valueDescription = "your own"
	}
	fmt.Printf("Choose a number from below, or type in %s value.\n", valueDescription)
	attributes := []string{terminal.HiRedFg, terminal.HiGreenFg}
	for i, text := range defaults {
		var lines []string
		if help != nil {
			parts := strings.Split(help[i], "\n")
			lines = append(lines, parts...)
		}
		lines = append(lines, fmt.Sprintf("%q", text))
		pos := i + 1
		terminal.WriteString(attributes[i%len(attributes)])
		if len(lines) == 1 {
			fmt.Printf("%2d > %s\n", pos, text)
		} else {
			mid := (len(lines) - 1) / 2
			for i, line := range lines {
				var sep rune
				switch i {
				case 0:
					sep = '/'
				case len(lines) - 1:
					sep = '\\'
				default:
					sep = '|'
				}
				number := "  "
				if i == mid {
					number = fmt.Sprintf("%2d", pos)
				}
				fmt.Printf("%s %c %s\n", number, sep, line)
			}
		}
		terminal.WriteString(terminal.Reset)
	}
	for {
		fmt.Printf("%s> ", what)
		result := ReadLine()
		i, err := strconv.Atoi(result)
		if err != nil {
			if newOk {
				return result
			}
			for _, v := range defaults {
				if result == v {
					return result
				}
			}
			continue
		}
		if i >= 1 && i <= len(defaults) {
			return defaults[i-1]
		}
	}
}

// ChooseNumber asks the user to enter a number between min and max
// inclusive prompting them with what.
func ChooseNumber(what string, min, max int) int {
	for {
		fmt.Printf("%s> ", what)
		result := ReadLine()
		i, err := strconv.Atoi(result)
		if err != nil {
			fmt.Printf("Bad number: %v\n", err)
			continue
		}
		if i < min || i > max {
			fmt.Printf("Out of range - %d to %d inclusive\n", min, max)
			continue
		}
		return i
	}
}

// ShowRemotes shows an overview of the config file
func ShowRemotes() {
	remotes := LoadedData().GetSectionList()
	if len(remotes) == 0 {
		return
	}
	sort.Strings(remotes)
	fmt.Printf("%-20s %s\n", "Name", "Type")
	fmt.Printf("%-20s %s\n", "====", "====")
	for _, remote := range remotes {
		fmt.Printf("%-20s %s\n", remote, FileGet(remote, "type"))
	}
}

// ChooseRemote chooses a remote name
func ChooseRemote() string {
	remotes := LoadedData().GetSectionList()
	sort.Strings(remotes)
	return Choose("remote", remotes, nil, false)
}

// mustFindByName finds the RegInfo for the remote name passed in or
// exits with a fatal error.
func mustFindByName(name string) *fs.RegInfo {
	fsType := FileGet(name, "type")
	if fsType == "" {
		log.Fatalf("Couldn't find type of fs for %q", name)
	}
	return fs.MustFind(fsType)
}

// ShowRemote shows the contents of the remote
func ShowRemote(name string) {
	fmt.Printf("--------------------\n")
	fmt.Printf("[%s]\n", name)
	fs := mustFindByName(name)
	for _, key := range LoadedData().GetKeyList(name) {
		isPassword := false
		for _, option := range fs.Options {
			if option.Name == key && option.IsPassword {
				isPassword = true
				break
			}
		}
		value := FileGet(name, key)
		if isPassword && value != "" {
			fmt.Printf("%s = *** ENCRYPTED ***\n", key)
		} else {
			fmt.Printf("%s = %s\n", key, value)
		}
	}
	fmt.Printf("--------------------\n")
}

// OkRemote prints the contents of the remote and ask if it is OK
func OkRemote(name string) bool {
	ShowRemote(name)
	switch i := CommandDefault([]string{"yYes this is OK", "eEdit this remote", "dDelete this remote"}, 0); i {
	case 'y':
		return true
	case 'e':
		return false
	case 'd':
		LoadedData().DeleteSection(name)
		return true
	default:
		fs.Errorf(nil, "Bad choice %c", i)
	}
	return false
}

// backendConfig configures the backend starting from the state passed in
//
// The is the user interface loop that drives the post configuration backend config.
func backendConfig(ctx context.Context, name string, m configmap.Mapper, ri *fs.RegInfo, choices configmap.Getter, startState string) error {
	in := fs.ConfigIn{
		State: startState,
	}
	for {
		out, err := fs.BackendConfig(ctx, name, m, ri, choices, in)
		if err != nil {
			return err
		}
		if out == nil {
			break
		}
		if out.Error != "" {
			fmt.Println(out.Error)
		}
		in.State = out.State
		in.Result = out.Result
		if out.Option != nil {
			fs.Debugf(name, "config: reading config parameter %q", out.Option.Name)
			if out.Option.Default == nil {
				out.Option.Default = ""
			}
			if Default, isBool := out.Option.Default.(bool); isBool &&
				len(out.Option.Examples) == 2 &&
				out.Option.Examples[0].Help == "Yes" &&
				out.Option.Examples[0].Value == "true" &&
				out.Option.Examples[1].Help == "No" &&
				out.Option.Examples[1].Value == "false" &&
				out.Option.Exclusive {
				// Use Confirm for Yes/No questions as it has a nicer interface=
				fmt.Println(out.Option.Help)
				in.Result = fmt.Sprint(Confirm(Default))
			} else {
				value := ChooseOption(out.Option)
				if value != "" {
					err := out.Option.Set(value)
					if err != nil {
						return fmt.Errorf("failed to set option: %w", err)
					}
				}
				in.Result = out.Option.String()
			}
		}
		if out.State == "" {
			break
		}
	}
	return nil
}

// PostConfig configures the backend after the main config has been done
//
// The is the user interface loop that drives the post configuration backend config.
func PostConfig(ctx context.Context, name string, m configmap.Mapper, ri *fs.RegInfo) error {
	if ri.Config == nil {
		return errors.New("backend doesn't support reconnect or authorize")
	}
	return backendConfig(ctx, name, m, ri, configmap.Simple{}, "")
}

// RemoteConfig runs the config helper for the remote if needed
func RemoteConfig(ctx context.Context, name string) error {
	fmt.Printf("Remote config\n")
	ri := mustFindByName(name)
	m := fs.ConfigMap(ri, name, nil)
	if ri.Config == nil {
		return nil
	}
	return PostConfig(ctx, name, m, ri)
}

// ChooseOption asks the user to choose an option
func ChooseOption(o *fs.Option) string {
	fmt.Printf("Option %s.\n", o.Name)
	if o.Help != "" {
		// Show help string without empty lines.
		help := strings.Replace(strings.TrimSpace(o.Help), "\n\n", "\n", -1)
		fmt.Println(help)
	}
	if o.IsPassword {
		fmt.Printf("Choose an alternative below.")
		actions := []string{"yYes type in my own password", "gGenerate random password"}
		defaultAction := -1
		if !o.Required {
			defaultAction = len(actions)
			actions = append(actions, "nNo leave this optional password blank")
			fmt.Printf(" Press Enter for the default (%s).", string(actions[defaultAction][0]))
		}
		fmt.Println()
		var password string
		var err error
		switch i := CommandDefault(actions, defaultAction); i {
		case 'y':
			password = ChangePassword("the")
		case 'g':
			for {
				fmt.Printf("Password strength in bits.\n64 is just about memorable\n128 is secure\n1024 is the maximum\n")
				bits := ChooseNumber("Bits", 64, 1024)
				password, err = Password(bits)
				if err != nil {
					log.Fatalf("Failed to make password: %v", err)
				}
				fmt.Printf("Your password is: %s\n", password)
				fmt.Printf("Use this password? Please note that an obscured version of this \npassword (and not the " +
					"password itself) will be stored under your \nconfiguration file, so keep this generated password " +
					"in a safe place.\n")
				if Confirm(true) {
					break
				}
			}
		case 'n':
			return ""
		default:
			fs.Errorf(nil, "Bad choice %c", i)
		}
		return obscure.MustObscure(password)
	}
	what := fmt.Sprintf("%T value", o.Default)
	switch o.Default.(type) {
	case bool:
		what = "boolean value (true or false)"
	case fs.SizeSuffix:
		what = "size with suffix K,M,G,T"
	case fs.Duration:
		what = "duration s,m,h,d,w,M,y"
	case int, int8, int16, int32, int64:
		what = "signed integer"
	case uint, byte, uint16, uint32, uint64:
		what = "unsigned integer"
	}
	var in string
	for {
		fmt.Printf("Enter a %s. Press Enter for the default (%q).\n", what, fmt.Sprint(o.Default))
		if len(o.Examples) > 0 {
			var values []string
			var help []string
			for _, example := range o.Examples {
				values = append(values, example.Value)
				help = append(help, example.Help)
			}
			in = Choose(o.Name, values, help, !o.Exclusive)
		} else {
			fmt.Printf("%s> ", o.Name)
			in = ReadLine()
		}
		if in == "" {
			if o.Required && fmt.Sprint(o.Default) == "" {
				fmt.Printf("This value is required and it has no default.\n")
				continue
			}
			break
		}
		newIn, err := configstruct.StringToInterface(o.Default, in)
		if err != nil {
			fmt.Printf("Failed to parse %q: %v\n", in, err)
			continue
		}
		in = fmt.Sprint(newIn) // canonicalise
		break
	}
	return in
}

// NewRemoteName asks the user for a name for a new remote
func NewRemoteName() (name string) {
	for {
		fmt.Printf("name> ")
		name = ReadLine()
		if LoadedData().HasSection(name) {
			fmt.Printf("Remote %q already exists.\n", name)
			continue
		}
		err := fspath.CheckConfigName(name)
		switch {
		case name == "":
			fmt.Printf("Can't use empty name.\n")
		case driveletter.IsDriveLetter(name):
			fmt.Printf("Can't use %q as it can be confused with a drive letter.\n", name)
		case err != nil:
			fmt.Printf("Can't use %q as %v.\n", name, err)
		default:
			return name
		}
	}
}

// NewRemote make a new remote from its name
func NewRemote(ctx context.Context, name string) error {
	var (
		newType string
		ri      *fs.RegInfo
		err     error
	)

	// Set the type first
	for {
		newType = ChooseOption(fsOption())
		ri, err = fs.Find(newType)
		if err != nil {
			fmt.Printf("Bad remote %q: %v\n", newType, err)
			continue
		}
		break
	}
	LoadedData().SetValue(name, "type", newType)

	_, err = CreateRemote(ctx, name, newType, nil, UpdateRemoteOpt{
		All: true,
	})
	if err != nil {
		return err
	}
	if OkRemote(name) {
		SaveConfig()
		return nil
	}
	return EditRemote(ctx, ri, name)
}

// EditRemote gets the user to edit a remote
func EditRemote(ctx context.Context, ri *fs.RegInfo, name string) error {
	ShowRemote(name)
	fmt.Printf("Edit remote\n")
	for {
		_, err := UpdateRemote(ctx, name, nil, UpdateRemoteOpt{
			All: true,
		})
		if err != nil {
			return err
		}
		if OkRemote(name) {
			break
		}
	}
	SaveConfig()
	return nil
}

// DeleteRemote gets the user to delete a remote
func DeleteRemote(name string) {
	LoadedData().DeleteSection(name)
	SaveConfig()
}

// copyRemote asks the user for a new remote name and copies name into
// it. Returns the new name.
func copyRemote(name string) string {
	newName := NewRemoteName()
	// Copy the keys
	for _, key := range LoadedData().GetKeyList(name) {
		value := getWithDefault(name, key, "")
		LoadedData().SetValue(newName, key, value)
	}
	return newName
}

// RenameRemote renames a config section
func RenameRemote(name string) {
	fmt.Printf("Enter new name for %q remote.\n", name)
	newName := copyRemote(name)
	if name != newName {
		LoadedData().DeleteSection(name)
		SaveConfig()
	}
}

// CopyRemote copies a config section
func CopyRemote(name string) {
	fmt.Printf("Enter name for copy of %q remote.\n", name)
	copyRemote(name)
	SaveConfig()
}

// ShowConfigLocation prints the location of the config file in use
func ShowConfigLocation() {
	if configPath := GetConfigPath(); configPath == "" {
		fmt.Println("Configuration is in memory only")
	} else {
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Println("Configuration file doesn't exist, but rclone will use this path:")
		} else {
			fmt.Println("Configuration file is stored at:")
		}
		fmt.Printf("%s\n", configPath)
	}
}

// ShowConfig prints the (unencrypted) config options
func ShowConfig() {
	str, err := LoadedData().Serialize()
	if err != nil {
		log.Fatalf("Failed to serialize config: %v", err)
	}
	if str == "" {
		str = "; empty config\n"
	}
	fmt.Printf("%s", str)
}

// EditConfig edits the config file interactively
func EditConfig(ctx context.Context) (err error) {
	for {
		haveRemotes := len(LoadedData().GetSectionList()) != 0
		what := []string{"eEdit existing remote", "nNew remote", "dDelete remote", "rRename remote", "cCopy remote", "sSet configuration password", "qQuit config"}
		if haveRemotes {
			fmt.Printf("Current remotes:\n\n")
			ShowRemotes()
			fmt.Printf("\n")
		} else {
			fmt.Printf("No remotes found - make a new one\n")
			// take 2nd item and last 2 items of menu list
			what = append(what[1:2], what[len(what)-2:]...)
		}
		switch i := Command(what); i {
		case 'e':
			name := ChooseRemote()
			fs := mustFindByName(name)
			err = EditRemote(ctx, fs, name)
			if err != nil {
				return err
			}
		case 'n':
			err = NewRemote(ctx, NewRemoteName())
			if err != nil {
				return err
			}
		case 'd':
			name := ChooseRemote()
			DeleteRemote(name)
		case 'r':
			RenameRemote(ChooseRemote())
		case 'c':
			CopyRemote(ChooseRemote())
		case 's':
			SetPassword()
		case 'q':
			return nil
		}
	}
}

// Suppress the confirm prompts by altering the context config
func suppressConfirm(ctx context.Context) context.Context {
	newCtx, ci := fs.AddConfig(ctx)
	ci.AutoConfirm = true
	return newCtx
}

// checkPassword normalises and validates the password
func checkPassword(password string) (string, error) {
	if !utf8.ValidString(password) {
		return "", errors.New("password contains invalid utf8 characters")
	}
	// Check for leading/trailing whitespace
	trimmedPassword := strings.TrimSpace(password)
	// Warn user if password has leading+trailing whitespace
	if len(password) != len(trimmedPassword) {
		_, _ = fmt.Fprintln(os.Stderr, "Your password contains leading/trailing whitespace - in previous versions of rclone this was stripped")
	}
	// Normalize to reduce weird variations.
	password = norm.NFKC.String(password)
	if len(password) == 0 || len(trimmedPassword) == 0 {
		return "", errors.New("no characters in password")
	}
	return password, nil
}

// GetPassword asks the user for a password with the prompt given.
func GetPassword(prompt string) string {
	_, _ = fmt.Fprintln(PasswordPromptOutput, prompt)
	for {
		_, _ = fmt.Fprint(PasswordPromptOutput, "password:")
		password := ReadPassword()
		password, err := checkPassword(password)
		if err == nil {
			return password
		}
		_, _ = fmt.Fprintf(os.Stderr, "Bad password: %v\n", err)
	}
}

// ChangePassword will query the user twice for the named password. If
// the same password is entered it is returned.
func ChangePassword(name string) string {
	for {
		a := GetPassword(fmt.Sprintf("Enter %s password:", name))
		b := GetPassword(fmt.Sprintf("Confirm %s password:", name))
		if a == b {
			return a
		}
		fmt.Println("Passwords do not match!")
	}
}

// SetPassword will allow the user to modify the current
// configuration encryption settings.
func SetPassword() {
	for {
		if len(configKey) > 0 {
			fmt.Println("Your configuration is encrypted.")
			what := []string{"cChange Password", "uUnencrypt configuration", "qQuit to main menu"}
			switch i := Command(what); i {
			case 'c':
				changeConfigPassword()
				SaveConfig()
				fmt.Println("Password changed")
				continue
			case 'u':
				configKey = nil
				SaveConfig()
				continue
			case 'q':
				return
			}

		} else {
			fmt.Println("Your configuration is not encrypted.")
			fmt.Println("If you add a password, you will protect your login information to cloud services.")
			what := []string{"aAdd Password", "qQuit to main menu"}
			switch i := Command(what); i {
			case 'a':
				changeConfigPassword()
				SaveConfig()
				fmt.Println("Password set")
				continue
			case 'q':
				return
			}
		}
	}
}
