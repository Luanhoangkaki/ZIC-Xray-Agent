package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/durable"
)

// There is intentionally NO send/retry/enable command. Reconciliation only
// records an operator decision and never puts UNKNOWN back into PENDING.
func trafficCommand(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("traffic requires status, export, resolve or acknowledge-gap; sending is disabled")
	}
	command := args[0]
	f := flag.NewFlagSet("traffic", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	dir := f.String("state-dir", "/var/lib/zic-xray-agent/data", "private state directory")
	id := f.String("id", "", "batch/incident ID")
	digest := f.String("digest", "", "expected immutable payload digest")
	decision := f.String("decision", "", "applied or not-applied")
	evidence := f.String("evidence", "", "non-sensitive evidence/ticket reference")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 {
		return errors.New("invalid traffic command arguments")
	}
	if command != "status" && command != "export" && command != "resolve" && command != "acknowledge-gap" {
		return errors.New("traffic sending/retry commands are unavailable")
	}
	path := filepath.Join(*dir, "traffic.json")
	if command == "status" {
		l, err := accounting.Inspect(path)
		if err != nil {
			return errors.New("no readable private traffic checkpoint")
		}
		type batch struct {
			ID       string                `json:"id"`
			Digest   string                `json:"digest"`
			State    accounting.BatchState `json:"state"`
			Attempts int                   `json:"attempts"`
			Users    int                   `json:"users"`
		}
		summary := struct {
			Sending         bool     `json:"sending_enabled"`
			ProductionReady bool     `json:"production_ready"`
			Batches         []batch  `json:"batches"`
			OpenIncidents   []string `json:"open_incident_ids"`
		}{Batches: []batch{}, OpenIncidents: []string{}}
		for _, b := range l.Queue {
			summary.Batches = append(summary.Batches, batch{b.ID, b.Digest, b.State, b.Attempts, len(b.Payload)})
		}
		for _, incident := range l.Incidents {
			if !incident.Resolved {
				summary.OpenIncidents = append(summary.OpenIncidents, incident.ID)
			}
		}
		return json.NewEncoder(output).Encode(summary)
	}
	unlock, err := durable.Lock(filepath.Join(*dir, "agent.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	view, err := accounting.Inspect(path)
	if err != nil {
		return errors.New("no readable private traffic checkpoint")
	}
	l, err := accounting.Open(path, view.Identity)
	if err != nil {
		return err
	}
	switch command {
	case "resolve":
		if *decision != "applied" && *decision != "not-applied" {
			return errors.New("explicit applied/not-applied decision required")
		}
		return l.ResolveUnknown(path, *id, *digest, *decision == "applied", *evidence)
	case "acknowledge-gap":
		return l.ResolveIncident(path, *id, *evidence)
	case "export":
		for _, b := range l.Queue {
			if b.ID == *id {
				if b.Digest != *digest || b.State == accounting.Pending || b.State == accounting.Sending {
					return errors.New("export requires a matching frozen attempted batch; never send an export automatically")
				}
				destination := filepath.Join(*dir, "exports")
				if err := durable.PrivateDir(destination); err != nil {
					return err
				}
				file := filepath.Join(destination, b.ID+"-"+b.Digest+".json")
				if _, err := os.Lstat(file); !os.IsNotExist(err) {
					return errors.New("export already exists or is inaccessible")
				}
				raw, err := json.MarshalIndent(b, "", "  ")
				if err != nil {
					return err
				}
				if err := durable.Write(file, raw); err != nil {
					return err
				}
				return nil
			}
		}
		return errors.New("batch not found")
	}
	return errors.New("unknown traffic action")
}
