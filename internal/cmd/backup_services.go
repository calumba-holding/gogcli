package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/drive/v3"
	gapi "google.golang.org/api/googleapi"
	"google.golang.org/api/people/v1"
	"google.golang.org/api/tasks/v1"

	"github.com/steipete/gogcli/internal/backup"
)

type calendarBackupEvent struct {
	CalendarID string          `json:"calendarId"`
	Event      *calendar.Event `json:"event"`
}

type contactsBackupPerson struct {
	Source string         `json:"source"`
	Person *people.Person `json:"person"`
}

type driveBackupFile struct {
	File *drive.File `json:"file"`
}

type tasksBackupTask struct {
	TaskListID string      `json:"taskListId"`
	Task       *tasks.Task `json:"task"`
}

func expandBackupServices(services []string) []string {
	var out []string
	for _, service := range services {
		if strings.EqualFold(strings.TrimSpace(service), "all") {
			out = append(out, backupServiceCalendar, backupServiceContacts, backupServiceDrive, backupServiceGmail, backupServiceTasks)
			continue
		}
		out = append(out, service)
	}
	return out
}

func buildCalendarBackupSnapshot(ctx context.Context, flags *RootFlags, shardMaxRows int) (backup.Snapshot, error) {
	account, err := requireAccount(flags)
	if err != nil {
		return backup.Snapshot{}, err
	}
	svc, err := newCalendarService(ctx, account)
	if err != nil {
		return backup.Snapshot{}, err
	}
	accountHash := backupAccountHash(account)
	calendars, err := fetchBackupCalendars(ctx, svc)
	if err != nil {
		return backup.Snapshot{}, err
	}
	events, err := fetchBackupCalendarEvents(ctx, svc, calendars)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards := make([]backup.PlainShard, 0, 2)
	calendarShard, err := backup.NewJSONLShard(backupServiceCalendar, "calendars", accountHash, fmt.Sprintf("data/calendar/%s/calendars.jsonl.gz.age", accountHash), calendars)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards = append(shards, calendarShard)
	eventShards, err := buildBackupShards(backupServiceCalendar, "events", accountHash, fmt.Sprintf("data/calendar/%s/events", accountHash), events, shardMaxRows)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards = append(shards, eventShards...)
	return backup.Snapshot{
		Services: []string{backupServiceCalendar},
		Accounts: []string{accountHash},
		Counts: map[string]int{
			"calendar.calendars": len(calendars),
			"calendar.events":    len(events),
		},
		Shards: shards,
	}, nil
}

func buildContactsBackupSnapshot(ctx context.Context, flags *RootFlags, shardMaxRows int) (backup.Snapshot, error) {
	account, err := requireAccount(flags)
	if err != nil {
		return backup.Snapshot{}, err
	}
	accountHash := backupAccountHash(account)
	var peopleRows []contactsBackupPerson
	contactsSvc, err := newPeopleContactsService(ctx, account)
	if err != nil {
		return backup.Snapshot{}, err
	}
	connections, err := fetchBackupConnections(ctx, contactsSvc)
	if err != nil {
		return backup.Snapshot{}, err
	}
	peopleRows = append(peopleRows, connections...)
	otherSvc, err := newPeopleOtherContactsService(ctx, account)
	if err != nil {
		return backup.Snapshot{}, err
	}
	otherContacts, err := fetchBackupOtherContacts(ctx, otherSvc)
	if err != nil {
		return backup.Snapshot{}, err
	}
	peopleRows = append(peopleRows, otherContacts...)
	shards, err := buildBackupShards(backupServiceContacts, "people", accountHash, fmt.Sprintf("data/contacts/%s/people", accountHash), peopleRows, shardMaxRows)
	if err != nil {
		return backup.Snapshot{}, err
	}
	return backup.Snapshot{
		Services: []string{backupServiceContacts},
		Accounts: []string{accountHash},
		Counts: map[string]int{
			"contacts.connections": len(connections),
			"contacts.other":       len(otherContacts),
			"contacts.people":      len(peopleRows),
		},
		Shards: shards,
	}, nil
}

func buildDriveBackupSnapshot(ctx context.Context, flags *RootFlags, shardMaxRows int) (backup.Snapshot, error) {
	account, err := requireAccount(flags)
	if err != nil {
		return backup.Snapshot{}, err
	}
	svc, err := newDriveService(ctx, account)
	if err != nil {
		return backup.Snapshot{}, err
	}
	accountHash := backupAccountHash(account)
	drives, err := fetchBackupSharedDrives(ctx, svc)
	if err != nil {
		return backup.Snapshot{}, err
	}
	files, err := fetchBackupDriveFiles(ctx, svc)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards := make([]backup.PlainShard, 0, 2)
	driveShard, err := backup.NewJSONLShard(backupServiceDrive, "drives", accountHash, fmt.Sprintf("data/drive/%s/drives.jsonl.gz.age", accountHash), drives)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards = append(shards, driveShard)
	fileShards, err := buildBackupShards(backupServiceDrive, "files", accountHash, fmt.Sprintf("data/drive/%s/files", accountHash), files, shardMaxRows)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards = append(shards, fileShards...)
	return backup.Snapshot{
		Services: []string{backupServiceDrive},
		Accounts: []string{accountHash},
		Counts: map[string]int{
			"drive.drives": len(drives),
			"drive.files":  len(files),
		},
		Shards: shards,
	}, nil
}

func buildTasksBackupSnapshot(ctx context.Context, flags *RootFlags, shardMaxRows int) (backup.Snapshot, error) {
	account, err := requireAccount(flags)
	if err != nil {
		return backup.Snapshot{}, err
	}
	svc, err := newTasksService(ctx, account)
	if err != nil {
		return backup.Snapshot{}, err
	}
	accountHash := backupAccountHash(account)
	lists, err := fetchBackupTaskLists(ctx, svc)
	if err != nil {
		return backup.Snapshot{}, err
	}
	tasksRows, err := fetchBackupTasks(ctx, svc, lists)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards := make([]backup.PlainShard, 0, 2)
	listShard, err := backup.NewJSONLShard(backupServiceTasks, "lists", accountHash, fmt.Sprintf("data/tasks/%s/lists.jsonl.gz.age", accountHash), lists)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards = append(shards, listShard)
	taskShards, err := buildBackupShards(backupServiceTasks, "tasks", accountHash, fmt.Sprintf("data/tasks/%s/tasks", accountHash), tasksRows, shardMaxRows)
	if err != nil {
		return backup.Snapshot{}, err
	}
	shards = append(shards, taskShards...)
	return backup.Snapshot{
		Services: []string{backupServiceTasks},
		Accounts: []string{accountHash},
		Counts: map[string]int{
			"tasks.lists": len(lists),
			"tasks.tasks": len(tasksRows),
		},
		Shards: shards,
	}, nil
}

func fetchBackupCalendars(ctx context.Context, svc *calendar.Service) ([]*calendar.CalendarListEntry, error) {
	var out []*calendar.CalendarListEntry
	pageToken := ""
	for {
		call := svc.CalendarList.List().MaxResults(250).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Items...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out, nil
}

func fetchBackupCalendarEvents(ctx context.Context, svc *calendar.Service, calendars []*calendar.CalendarListEntry) ([]calendarBackupEvent, error) {
	var out []calendarBackupEvent
	for _, cal := range calendars {
		if cal == nil || strings.TrimSpace(cal.Id) == "" {
			continue
		}
		pageToken := ""
		for {
			call := svc.Events.List(cal.Id).
				MaxResults(2500).
				ShowDeleted(true).
				SingleEvents(false).
				Context(ctx)
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return nil, fmt.Errorf("calendar %s events: %w", cal.Id, err)
			}
			for _, event := range resp.Items {
				out = append(out, calendarBackupEvent{CalendarID: cal.Id, Event: event})
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CalendarID == out[j].CalendarID {
			return out[i].Event.Id < out[j].Event.Id
		}
		return out[i].CalendarID < out[j].CalendarID
	})
	return out, nil
}

func fetchBackupConnections(ctx context.Context, svc *people.Service) ([]contactsBackupPerson, error) {
	var out []contactsBackupPerson
	pageToken := ""
	for {
		call := svc.People.Connections.List(peopleMeResource).
			PersonFields(contactsGetReadMask).
			PageSize(1000).
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, person := range resp.Connections {
			out = append(out, contactsBackupPerson{Source: "connections", Person: person})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return out, nil
}

func fetchBackupOtherContacts(ctx context.Context, svc *people.Service) ([]contactsBackupPerson, error) {
	const otherContactsBackupReadMask = "names,emailAddresses,phoneNumbers"

	var out []contactsBackupPerson
	pageToken := ""
	for {
		call := svc.OtherContacts.List().
			ReadMask(otherContactsBackupReadMask).
			PageSize(1000).
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, person := range resp.OtherContacts {
			out = append(out, contactsBackupPerson{Source: "other", Person: person})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return out, nil
}

func fetchBackupSharedDrives(ctx context.Context, svc *drive.Service) ([]*drive.Drive, error) {
	var out []*drive.Drive
	pageToken := ""
	for {
		call := svc.Drives.List().
			PageSize(100).
			Fields("nextPageToken, drives(id, name, createdTime, hidden, restrictions)").
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Drives...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out, nil
}

func fetchBackupDriveFiles(ctx context.Context, svc *drive.Service) ([]driveBackupFile, error) {
	var out []driveBackupFile
	pageToken := ""
	for {
		call := svc.Files.List().
			Q("trashed = false").
			PageSize(1000).
			OrderBy("modifiedTime desc").
			Fields(gapi.Field("nextPageToken, files(id, name, mimeType, size, createdTime, modifiedTime, viewedByMeTime, parents, owners, lastModifyingUser, webViewLink, webContentLink, description, starred, trashed, explicitlyTrashed, shared, ownedByMe, driveId, md5Checksum, sha1Checksum, sha256Checksum, originalFilename, fileExtension, exportLinks, appProperties, properties)")).
			Context(ctx)
		call = driveFilesListCallWithDriveSupport(call, true)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, file := range resp.Files {
			out = append(out, driveBackupFile{File: file})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return out, nil
}

func fetchBackupTaskLists(ctx context.Context, svc *tasks.Service) ([]*tasks.TaskList, error) {
	var out []*tasks.TaskList
	pageToken := ""
	for {
		call := svc.Tasklists.List().MaxResults(100).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Items...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out, nil
}

func fetchBackupTasks(ctx context.Context, svc *tasks.Service, lists []*tasks.TaskList) ([]tasksBackupTask, error) {
	var out []tasksBackupTask
	for _, list := range lists {
		if list == nil || strings.TrimSpace(list.Id) == "" {
			continue
		}
		pageToken := ""
		for {
			call := svc.Tasks.List(list.Id).
				MaxResults(100).
				ShowCompleted(true).
				ShowDeleted(true).
				ShowHidden(true).
				ShowAssigned(true).
				Context(ctx)
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return nil, fmt.Errorf("task list %s tasks: %w", list.Id, err)
			}
			for _, task := range resp.Items {
				out = append(out, tasksBackupTask{TaskListID: list.Id, Task: task})
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskListID == out[j].TaskListID {
			return out[i].Task.Id < out[j].Task.Id
		}
		return out[i].TaskListID < out[j].TaskListID
	})
	return out, nil
}

func buildBackupShards[T any](service, kind, accountHash, prefix string, rows []T, shardMaxRows int) ([]backup.PlainShard, error) {
	if shardMaxRows <= 0 {
		shardMaxRows = 1000
	}
	if len(rows) == 0 {
		shard, err := backup.NewJSONLShard(service, kind, accountHash, prefix+"/part-0001.jsonl.gz.age", rows)
		if err != nil {
			return nil, err
		}
		return []backup.PlainShard{shard}, nil
	}
	shards := make([]backup.PlainShard, 0, (len(rows)+shardMaxRows-1)/shardMaxRows)
	for part, start := 1, 0; start < len(rows); part, start = part+1, start+shardMaxRows {
		end := start + shardMaxRows
		if end > len(rows) {
			end = len(rows)
		}
		rel := fmt.Sprintf("%s/part-%04d.jsonl.gz.age", prefix, part)
		shard, err := backup.NewJSONLShard(service, kind, accountHash, rel, rows[start:end])
		if err != nil {
			return nil, err
		}
		shards = append(shards, shard)
	}
	return shards, nil
}
