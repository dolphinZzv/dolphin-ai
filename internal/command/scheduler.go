package command

import (
	"github.com/spf13/cobra"

	"dolphin/internal/scheduler"
)

// RegisterScheduler registers the /scheduler command.
func RegisterScheduler(r *Registry, schedLister interface{ List() []*scheduler.Task }) {
	r.Register(WithI18nShort(&cobra.Command{
		Use: "scheduler",
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks := schedLister.List()
			if len(tasks) == 0 {
				cmd.Println("No scheduled tasks")
				return nil
			}
			if RenderModeFrom(cmd) == "markdown" {
				cmd.Print("**Scheduled tasks:**\n\n")
				cmd.Println("| Name | ID | Status | Type | Schedule | Command | Last Run | Runs |")
				cmd.Println("|------|----|--------|------|----------|---------|----------|------|")
				for _, t := range tasks {
					sched := t.Schedule
					typ := "cron"
					if t.Delay != "" {
						sched = t.Delay
						typ = "delay"
					}
					status := "pending"
					if t.LastStatus != "" {
						status = t.LastStatus
					}
					if !t.Enabled {
						status = "disabled"
					}
					lastRun := "-"
					if t.LastRunAt != nil {
						lastRun = t.LastRunAt.Format("2006-01-02 15:04:05")
					}
					cmd.Printf("| %s | %s | %s | %s | %s | %s | %s | %d |\n",
						t.Name, t.ID, status, typ, sched, t.Command, lastRun, t.RunCount)
				}
			} else {
				cmd.Println("Scheduled tasks:")
				for _, t := range tasks {
					sched := t.Schedule
					typ := "cron"
					if t.Delay != "" {
						sched = t.Delay
						typ = "delay"
					}
					status := "pending"
					if t.LastStatus != "" {
						status = t.LastStatus
					}
					if !t.Enabled {
						status = "disabled"
					}
					lastRun := "-"
					if t.LastRunAt != nil {
						lastRun = t.LastRunAt.Format("2006-01-02 15:04:05")
					}
					cmd.Printf("  %s (id: %s) [%s] %s: %s, cmd: %s, last: %s, runs: %d\n",
						t.Name, t.ID, status, typ, sched, t.Command, lastRun, t.RunCount)
				}
			}
			return nil
		},
	}, "command.scheduler_list_desc"))
}
