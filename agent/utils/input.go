package utils

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const stdinAnnotation = "stdin"

func MarkStdinLine(cmd *cobra.Command, name string) error {
	return cmd.Flags().SetAnnotation(name, stdinAnnotation, []string{"line"})
}

func MarkStdinStream(cmd *cobra.Command, name string) error {
	return cmd.Flags().SetAnnotation(name, stdinAnnotation, []string{"stream"})
}

func ResolveStdin(cmd *cobra.Command) error {
	var target *pflag.Flag
	var mode string
	var err error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		modes, ok := f.Annotations[stdinAnnotation]
		if !ok || len(modes) == 0 || !f.Changed || f.Value.String() != "-" {
			return
		}
		if target != nil {
			err = fmt.Errorf("only one flag can read stdin: --%s and --%s were both given -", target.Name, f.Name)
			return
		}
		target, mode = f, modes[0]
	})
	if err != nil || target == nil {
		return err
	}
	if StdinIsTerminal {
		return fmt.Errorf("--%s was given - but nothing is piped into stdin", target.Name)
	}

	var value string
	if mode == "line" {
		line, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		value = strings.TrimRight(line, "\r\n")
	} else {
		data, readErr := io.ReadAll(os.Stdin)
		if readErr != nil {
			return readErr
		}
		value = strings.TrimRight(string(data), "\r\n")
	}
	if value == "" {
		return fmt.Errorf("--%s was given - but stdin was empty", target.Name)
	}
	return target.Value.Set(value)
}
