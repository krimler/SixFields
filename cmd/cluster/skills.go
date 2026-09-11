package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"sixfields/internal/msg"
)

// skills ship with the binary so the agent a user already runs can drive this
// tool. They are instructions plus the commands above, not a chatbot.
//
//go:embed all:skills
var skillFiles embed.FS

func newSkillsCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "skills install",
		Short: "Install the agent skills that drive this CLI",
		Long: "The skills are read-only: they run `cluster status --json`, `cluster why --json`\n" +
			"and `cluster render`, and they are told to ground every claim in that output.\n" +
			"Nothing is sent anywhere; the agent is the user's own.",
		Example: "  cluster skills install\n  cluster skills install --dir ~/.config/agent/skills",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "install" {
				return &msg.Error{Code: msg.ClusterNotFound,
					Summary: "the only subcommand is `install`.", Action: "cluster skills install"}
			}
			entries, err := skillFiles.ReadDir("skills")
			if err != nil {
				return err
			}
			for _, entry := range entries {
				name := entry.Name()
				b, err := skillFiles.ReadFile("skills/" + name + "/SKILL.md")
				if err != nil {
					continue
				}
				target := filepath.Join(dir, name)
				if err := os.MkdirAll(target, 0o755); err != nil {
					return err
				}
				path := filepath.Join(target, "SKILL.md")
				if err := os.WriteFile(path, b, 0o644); err != nil {
					return err
				}
				outln(cmd, "installed "+path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".claude/skills", "where to install the skills")
	return cmd
}
