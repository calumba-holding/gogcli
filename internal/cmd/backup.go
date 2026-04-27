package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/gmail/v1"

	"github.com/steipete/gogcli/internal/backup"
	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

type BackupCmd struct {
	Init   BackupInitCmd   `cmd:"" name:"init" help:"Initialize encrypted backup config and repository"`
	Push   BackupPushCmd   `cmd:"" name:"push" help:"Export services into encrypted backup shards"`
	Status BackupStatusCmd `cmd:"" name:"status" help:"Inspect backup manifest without decrypting shards"`
	Verify BackupVerifyCmd `cmd:"" name:"verify" help:"Decrypt and verify all backup shards"`
	Cat    BackupCatCmd    `cmd:"" name:"cat" help:"Decrypt one backup shard to stdout"`
	Export BackupExportCmd `cmd:"" name:"export" help:"Write a local plaintext export"`
	Gmail  BackupGmailCmd  `cmd:"" name:"gmail" help:"Gmail backup operations"`
}

type BackupGmailCmd struct {
	Push BackupGmailPushCmd `cmd:"" name:"push" help:"Export Gmail into encrypted backup shards"`
}

const (
	backupServiceCalendar = "calendar"
	backupServiceContacts = "contacts"
	backupServiceDrive    = "drive"
	backupServiceGmail    = "gmail"
	backupServiceTasks    = "tasks"
)

type backupFlags struct {
	Config     string   `name:"config" help:"Backup config path" default:""`
	Repo       string   `name:"repo" help:"Local backup repository path"`
	Remote     string   `name:"remote" help:"Backup Git remote URL"`
	Identity   string   `name:"identity" help:"Local age identity path"`
	Recipients []string `name:"recipient" help:"Public age recipient (repeatable)"`
	NoPush     bool     `name:"no-push" help:"Commit locally but do not push to the remote"`
}

func (f backupFlags) options() backup.Options {
	return backup.Options{
		ConfigPath: f.Config,
		Repo:       f.Repo,
		Remote:     f.Remote,
		Identity:   f.Identity,
		Recipients: f.Recipients,
		Push:       !f.NoPush,
	}
}

type backupReadFlags struct {
	Config   string `name:"config" help:"Backup config path" default:""`
	Repo     string `name:"repo" help:"Local backup repository path"`
	Remote   string `name:"remote" help:"Backup Git remote URL"`
	Identity string `name:"identity" help:"Local age identity path"`
}

func (f backupReadFlags) options() backup.Options {
	return backup.Options{
		ConfigPath: f.Config,
		Repo:       f.Repo,
		Remote:     f.Remote,
		Identity:   f.Identity,
		Push:       false,
	}
}

type BackupInitCmd struct {
	backupFlags
}

func (c *BackupInitCmd) Run(ctx context.Context) error {
	cfg, recipient, err := backup.Init(ctx, c.options())
	if err != nil {
		return err
	}
	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"repo":      cfg.Repo,
			"remote":    cfg.Remote,
			"identity":  cfg.Identity,
			"recipient": recipient,
		})
	}
	u := ui.FromContext(ctx)
	u.Out().Printf("repo\t%s", cfg.Repo)
	u.Out().Printf("remote\t%s", cfg.Remote)
	u.Out().Printf("identity\t%s", cfg.Identity)
	u.Out().Printf("recipient\t%s", recipient)
	return nil
}

type BackupPushCmd struct {
	backupFlags
	Services         string `name:"services" help:"Comma-separated services to back up (currently: gmail)" default:"gmail"`
	Query            string `name:"query" help:"Gmail query for bounded/test backups"`
	Max              int64  `name:"max" aliases:"limit" help:"Max Gmail messages to export; 0 means all" default:"0"`
	IncludeSpamTrash bool   `name:"include-spam-trash" help:"Include Gmail spam and trash" default:"true"`
	ShardMaxRows     int    `name:"shard-max-rows" help:"Max rows per encrypted shard" default:"1000"`
}

func (c *BackupPushCmd) Run(ctx context.Context, flags *RootFlags) error {
	services := expandBackupServices(splitCSV(c.Services))
	if len(services) == 0 {
		return usage("at least one --services value is required")
	}
	var snapshots []backup.Snapshot
	for _, service := range services {
		switch strings.ToLower(strings.TrimSpace(service)) {
		case backupServiceCalendar:
			snapshot, err := buildCalendarBackupSnapshot(ctx, flags, c.ShardMaxRows)
			if err != nil {
				return err
			}
			snapshots = append(snapshots, snapshot)
		case backupServiceContacts:
			snapshot, err := buildContactsBackupSnapshot(ctx, flags, c.ShardMaxRows)
			if err != nil {
				return err
			}
			snapshots = append(snapshots, snapshot)
		case backupServiceDrive:
			snapshot, err := buildDriveBackupSnapshot(ctx, flags, c.ShardMaxRows)
			if err != nil {
				return err
			}
			snapshots = append(snapshots, snapshot)
		case backupServiceGmail:
			snapshot, err := buildGmailBackupSnapshot(ctx, flags, gmailBackupOptions{
				Query:            c.Query,
				Max:              c.Max,
				IncludeSpamTrash: c.IncludeSpamTrash,
				ShardMaxRows:     c.ShardMaxRows,
			})
			if err != nil {
				return err
			}
			snapshots = append(snapshots, snapshot)
		case backupServiceTasks:
			snapshot, err := buildTasksBackupSnapshot(ctx, flags, c.ShardMaxRows)
			if err != nil {
				return err
			}
			snapshots = append(snapshots, snapshot)
		default:
			return fmt.Errorf("unsupported backup service %q (supported: all, calendar, contacts, drive, gmail, tasks)", service)
		}
	}
	result, err := backup.PushSnapshot(ctx, mergeBackupSnapshots(snapshots...), c.options())
	if err != nil {
		return err
	}
	return writeBackupResult(ctx, result)
}

type BackupGmailPushCmd struct {
	backupFlags
	Query            string `name:"query" help:"Gmail query for bounded/test backups"`
	Max              int64  `name:"max" aliases:"limit" help:"Max Gmail messages to export; 0 means all" default:"0"`
	IncludeSpamTrash bool   `name:"include-spam-trash" help:"Include spam and trash" default:"true"`
	ShardMaxRows     int    `name:"shard-max-rows" help:"Max messages per encrypted shard" default:"1000"`
}

func (c *BackupGmailPushCmd) Run(ctx context.Context, flags *RootFlags) error {
	snapshot, err := buildGmailBackupSnapshot(ctx, flags, gmailBackupOptions{
		Query:            c.Query,
		Max:              c.Max,
		IncludeSpamTrash: c.IncludeSpamTrash,
		ShardMaxRows:     c.ShardMaxRows,
	})
	if err != nil {
		return err
	}
	result, err := backup.PushSnapshot(ctx, snapshot, c.options())
	if err != nil {
		return err
	}
	return writeBackupResult(ctx, result)
}

type BackupStatusCmd struct {
	backupFlags
}

func (c *BackupStatusCmd) Run(ctx context.Context) error {
	manifest, repo, err := backup.Status(ctx, c.options())
	if err != nil {
		return err
	}
	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{"repo": repo, "manifest": manifest})
	}
	u := ui.FromContext(ctx)
	u.Out().Printf("repo\t%s", repo)
	u.Out().Printf("encrypted\t%t", manifest.Encrypted)
	u.Out().Printf("exported\t%s", manifest.Exported.Format(time.RFC3339))
	u.Out().Printf("services\t%s", strings.Join(manifest.Services, ","))
	u.Out().Printf("accounts\t%s", strings.Join(manifest.Accounts, ","))
	u.Out().Printf("shards\t%d", len(manifest.Shards))
	keys := make([]string, 0, len(manifest.Counts))
	for key := range manifest.Counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		u.Out().Printf("count.%s\t%d", key, manifest.Counts[key])
	}
	return nil
}

type BackupVerifyCmd struct {
	backupFlags
}

func (c *BackupVerifyCmd) Run(ctx context.Context) error {
	result, err := backup.Verify(ctx, c.options())
	if err != nil {
		return err
	}
	return writeBackupResult(ctx, result)
}

type gmailBackupOptions struct {
	Query            string
	Max              int64
	IncludeSpamTrash bool
	ShardMaxRows     int
}

type gmailBackupMessage struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId,omitempty"`
	HistoryID    string   `json:"historyId,omitempty"`
	InternalDate int64    `json:"internalDate,omitempty"`
	LabelIDs     []string `json:"labelIds,omitempty"`
	SizeEstimate int64    `json:"sizeEstimate,omitempty"`
	Raw          string   `json:"raw"`
}

type gmailBackupLabel struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Type                  string `json:"type,omitempty"`
	MessageListVisibility string `json:"messageListVisibility,omitempty"`
	LabelListVisibility   string `json:"labelListVisibility,omitempty"`
	MessagesTotal         int64  `json:"messagesTotal,omitempty"`
	MessagesUnread        int64  `json:"messagesUnread,omitempty"`
	ThreadsTotal          int64  `json:"threadsTotal,omitempty"`
	ThreadsUnread         int64  `json:"threadsUnread,omitempty"`
}

func buildGmailBackupSnapshot(ctx context.Context, flags *RootFlags, opts gmailBackupOptions) (backup.Snapshot, error) {
	if opts.ShardMaxRows <= 0 {
		opts.ShardMaxRows = 1000
	}
	account, err := requireAccount(flags)
	if err != nil {
		return backup.Snapshot{}, err
	}
	svc, err := newGmailService(ctx, account)
	if err != nil {
		return backup.Snapshot{}, err
	}
	accountHash := backupAccountHash(account)
	labels, err := fetchGmailBackupLabels(ctx, svc)
	if err != nil {
		return backup.Snapshot{}, err
	}
	messages, err := fetchGmailBackupMessages(ctx, svc, opts)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards := make([]backup.PlainShard, 0, 1)
	labelShard, err := backup.NewJSONLShard(backupServiceGmail, "labels", accountHash, fmt.Sprintf("data/gmail/%s/labels.jsonl.gz.age", accountHash), labels)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards = append(shards, labelShard)
	messageShards, err := buildGmailMessageShards(accountHash, messages, opts.ShardMaxRows)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards = append(shards, messageShards...)
	return backup.Snapshot{
		Services: []string{backupServiceGmail},
		Accounts: []string{accountHash},
		Counts: map[string]int{
			"gmail.labels":   len(labels),
			"gmail.messages": len(messages),
		},
		Shards: shards,
	}, nil
}

func fetchGmailBackupLabels(ctx context.Context, svc *gmail.Service) ([]gmailBackupLabel, error) {
	resp, err := svc.Users.Labels.List("me").Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	out := make([]gmailBackupLabel, 0, len(resp.Labels))
	for _, label := range resp.Labels {
		if label == nil {
			continue
		}
		out = append(out, gmailBackupLabel{
			ID:                    label.Id,
			Name:                  label.Name,
			Type:                  label.Type,
			MessageListVisibility: label.MessageListVisibility,
			LabelListVisibility:   label.LabelListVisibility,
			MessagesTotal:         label.MessagesTotal,
			MessagesUnread:        label.MessagesUnread,
			ThreadsTotal:          label.ThreadsTotal,
			ThreadsUnread:         label.ThreadsUnread,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func fetchGmailBackupMessages(ctx context.Context, svc *gmail.Service, opts gmailBackupOptions) ([]gmailBackupMessage, error) {
	ids, err := listGmailBackupMessageIDs(ctx, svc, opts)
	if err != nil {
		return nil, err
	}
	const maxConcurrency = 8
	sem := make(chan struct{}, maxConcurrency)
	type result struct {
		index int
		msg   gmailBackupMessage
		err   error
	}
	results := make(chan result, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(index int, messageID string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- result{index: index, err: ctx.Err()}
				return
			}
			msg, err := svc.Users.Messages.Get("me", messageID).
				Format(gmailFormatRaw).
				Fields("id,threadId,historyId,internalDate,labelIds,sizeEstimate,raw").
				Context(ctx).
				Do()
			if err != nil {
				results <- result{index: index, err: fmt.Errorf("gmail message %s: %w", messageID, err)}
				return
			}
			if strings.TrimSpace(msg.Raw) == "" {
				results <- result{index: index, err: fmt.Errorf("gmail message %s returned empty raw payload", messageID)}
				return
			}
			results <- result{index: index, msg: gmailBackupMessage{
				ID:           msg.Id,
				ThreadID:     msg.ThreadId,
				HistoryID:    formatHistoryID(msg.HistoryId),
				InternalDate: msg.InternalDate,
				LabelIDs:     append([]string(nil), msg.LabelIds...),
				SizeEstimate: msg.SizeEstimate,
				Raw:          msg.Raw,
			}}
		}(i, id)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	ordered := make([]gmailBackupMessage, len(ids))
	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		ordered[res.index] = res.msg
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return ordered, nil
}

func listGmailBackupMessageIDs(ctx context.Context, svc *gmail.Service, opts gmailBackupOptions) ([]string, error) {
	var ids []string
	pageToken := ""
	for {
		maxResults := int64(500)
		if opts.Max > 0 {
			remaining := opts.Max - int64(len(ids))
			if remaining <= 0 {
				break
			}
			if remaining < maxResults {
				maxResults = remaining
			}
		}
		call := svc.Users.Messages.List("me").
			MaxResults(maxResults).
			IncludeSpamTrash(opts.IncludeSpamTrash).
			Fields("messages(id),nextPageToken").
			Context(ctx)
		if strings.TrimSpace(opts.Query) != "" {
			call = call.Q(opts.Query)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, message := range resp.Messages {
			if message != nil && strings.TrimSpace(message.Id) != "" {
				ids = append(ids, message.Id)
			}
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return ids, nil
}

func buildGmailMessageShards(accountHash string, messages []gmailBackupMessage, shardMaxRows int) ([]backup.PlainShard, error) {
	if shardMaxRows <= 0 {
		shardMaxRows = 1000
	}
	buckets := map[string][]gmailBackupMessage{}
	for _, message := range messages {
		t := time.UnixMilli(message.InternalDate).UTC()
		if message.InternalDate <= 0 {
			t = time.Unix(0, 0).UTC()
		}
		key := fmt.Sprintf("%04d/%02d", t.Year(), int(t.Month()))
		buckets[key] = append(buckets[key], message)
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	shards := make([]backup.PlainShard, 0, len(keys))
	for _, key := range keys {
		values := buckets[key]
		sort.Slice(values, func(i, j int) bool {
			if values[i].InternalDate == values[j].InternalDate {
				return values[i].ID < values[j].ID
			}
			return values[i].InternalDate < values[j].InternalDate
		})
		for part, start := 1, 0; start < len(values); part, start = part+1, start+shardMaxRows {
			end := start + shardMaxRows
			if end > len(values) {
				end = len(values)
			}
			rel := fmt.Sprintf("data/gmail/%s/messages/%s/part-%04d.jsonl.gz.age", accountHash, key, part)
			shard, err := backup.NewJSONLShard(backupServiceGmail, "messages", accountHash, rel, values[start:end])
			if err != nil {
				return nil, err
			}
			shards = append(shards, shard)
		}
	}
	return shards, nil
}

func backupAccountHash(account string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	return hex.EncodeToString(sum[:12])
}

func mergeBackupSnapshots(snapshots ...backup.Snapshot) backup.Snapshot {
	out := backup.Snapshot{Counts: map[string]int{}}
	for _, snapshot := range snapshots {
		out.Services = append(out.Services, snapshot.Services...)
		out.Accounts = append(out.Accounts, snapshot.Accounts...)
		out.Shards = append(out.Shards, snapshot.Shards...)
		for key, value := range snapshot.Counts {
			out.Counts[key] += value
		}
	}
	return out
}

func writeBackupResult(ctx context.Context, result backup.Result) error {
	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, result)
	}
	u := ui.FromContext(ctx)
	u.Out().Printf("repo\t%s", result.Repo)
	u.Out().Printf("changed\t%t", result.Changed)
	u.Out().Printf("encrypted\t%t", result.Encrypted)
	u.Out().Printf("shards\t%d", result.Shards)
	keys := make([]string, 0, len(result.Counts))
	for key := range result.Counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		u.Out().Printf("count.%s\t%s", key, strconv.Itoa(result.Counts[key]))
	}
	return nil
}
