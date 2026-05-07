package output

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/openshift/tls-scanner/internal/scanner"
)

func PrintDryRunResults(discovery scanner.DiscoveryResults) {
	skipped := discovery.SkippedPorts()

	statusCounts := map[scanner.ScanStatus]int{}
	for _, s := range skipped {
		statusCounts[s.Status]++
	}

	fmt.Printf(`========================================
DRY RUN: Discovery complete
========================================
Scan targets:          %d
Skipped (localhost):   %d
Skipped (no ports):    %d
Skipped (probe ports): %d
========================================

`, len(discovery.ScanJobs),
		statusCounts[scanner.StatusLocalhostOnly],
		statusCounts[scanner.StatusNoPorts],
		statusCounts[scanner.StatusProbePort],
	)

	if len(discovery.ScanJobs) > 0 {
		fmt.Printf("SCAN TARGETS:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "IP\tPort\tPod Name\tNamespace\tComponent")
		for _, job := range discovery.ScanJobs {
			component := "N/A"
			if job.Component != nil {
				component = job.Component.Component
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n",
				job.IP, job.Port, job.Pod.Name, job.Pod.Namespace, component)
		}
		w.Flush()
	}

	skippedWithPorts := 0
	for _, s := range skipped {
		if s.Status != scanner.StatusNoPorts {
			skippedWithPorts++
		}
	}

	if skippedWithPorts > 0 {
		fmt.Printf("\nSKIPPED PORTS:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "IP\tPort\tPod Name\tNamespace\tStatus\tReason")
		for _, s := range skipped {
			if s.Status == scanner.StatusNoPorts {
				continue
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
				s.IP, s.Port, s.PodName, s.PodNamespace,
				s.Status, s.Reason)
		}
		w.Flush()
	}
}

func PrintDryRunTargets(jobs []scanner.ScanJob) {
	fmt.Printf(`========================================
DRY RUN: %d targets
========================================

`, len(jobs))

	if len(jobs) == 0 {
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "IP\tPort")
	for _, job := range jobs {
		fmt.Fprintf(w, "%s\t%d\n", job.IP, job.Port)
	}
	w.Flush()
}
