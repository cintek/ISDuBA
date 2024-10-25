// This file is Free Software under the Apache-2.0 License
// without warranty, see README.md and LICENSES/Apache-2.0.txt for details.
//
// SPDX-License-Identifier: Apache-2.0
//
// SPDX-FileCopyrightText: 2024 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
// Software-Engineering: 2024 Intevation GmbH <https://intevation.de>

package sources

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ISDuBA/ISDuBA/pkg/config"
	"github.com/ISDuBA/ISDuBA/pkg/database"
	"github.com/csaf-poc/csaf_distribution/v3/csaf"
	"github.com/jackc/pgx/v5/pgxpool"
)

type (
	// NoSuchEntryError is returned if a given feed or source does not exists.
	NoSuchEntryError string
	// InvalidArgumentError is returned if a given argument is unsuited.
	InvalidArgumentError string
)

// Error implements [builtin.error].
func (nsee NoSuchEntryError) Error() string { return string(nsee) }

// Error implements [builtin.error].
func (iae InvalidArgumentError) Error() string { return string(iae) }

// Is supports [errors.Is].
func (NoSuchEntryError) Is(target error) bool {
	_, ok := target.(NoSuchEntryError)
	return ok
}

// Is supports [errors.Is].
func (InvalidArgumentError) Is(target error) bool {
	_, ok := target.(InvalidArgumentError)
	return ok
}

// refreshDuration is the fallback duration for feeds to be checked for refresh.
const refreshDuration = time.Minute

type downloadJob struct {
	l location
	f *feed
}

// Manager fetches advisories from sources.
type Manager struct {
	cfg  *config.Config
	db   *database.DB
	fns  chan func(*Manager)
	jobs chan downloadJob
	done bool
	rnd  *rand.Rand

	cipherKey []byte

	sources []*source

	pmdCache  *pmdCache
	keysCache *keysCache

	val csaf.RemoteValidator

	usedSlots int
	uniqueID  int64
}

// SourceUpdateResult is return by UpdateSource.
type SourceUpdateResult int

const (
	// SourceUnchanged is returned if there was no change in the source.
	SourceUnchanged SourceUpdateResult = iota
	// SourceUpdated is returned if the source was updated.
	SourceUpdated
	// SourceDeactivated is returned if the source was deactivated during the update.
	SourceDeactivated
)

// Stats are some statistics about feeds and sources.
type Stats struct {
	Downloading int `json:"downloading"`
	Waiting     int `json:"waiting"`
}

// SourceInfo are infos about a source.
type SourceInfo struct {
	ID                      int64
	Name                    string
	URL                     string
	Active                  bool
	Status                  []string
	Rate                    *float64
	Slots                   *int
	Headers                 []string
	StrictMode              *bool
	Insecure                *bool
	SignatureCheck          *bool
	Age                     *time.Duration
	IgnorePatterns          []*regexp.Regexp
	HasClientCertPublic     bool
	HasClientCertPrivate    bool
	HasClientCertPassphrase bool
	Stats                   *Stats
}

// FeedInfo are infos about a feed.
type FeedInfo struct {
	ID    int64
	Label string
	URL   *url.URL
	Rolie bool
	Lvl   config.FeedLogLevel
	Stats *Stats
}

func (sur SourceUpdateResult) String() string {
	switch sur {
	case SourceUnchanged:
		return "unchanged"
	case SourceUpdated:
		return "updated"
	case SourceDeactivated:
		return "deactivated"
	default:
		return fmt.Sprintf("unknown update result: %d", sur)
	}
}

// NewManager creates a new downloader.
func NewManager(
	cfg *config.Config,
	db *database.DB,
	val csaf.RemoteValidator,
) (*Manager, error) {
	cipherKey, err := createCipherKey(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating cipher failed: %w", err)
	}
	return &Manager{
		cfg:       cfg,
		db:        db,
		fns:       make(chan func(*Manager)),
		jobs:      make(chan downloadJob),
		rnd:       rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
		cipherKey: cipherKey,
		pmdCache:  newPMDCache(),
		keysCache: newKeysCache(cfg.Sources.OpenPGPCaching),
		val:       val,
	}, nil
}

func (m *Manager) numActiveFeeds() int {
	sum := 0
	for _, s := range m.sources {
		if s.active {
			sum += len(s.feeds)
		}
	}
	return sum
}

func (m *Manager) activeFeeds(fn func(*feed) bool) {
	for _, s := range m.sources {
		if s.active {
			for _, f := range s.feeds {
				if !fn(f) {
					return
				}
			}
		}
	}
}

// shuffledActiveFeeds iterates in a shuffled order over
// the feeds of the active sources.
func (m *Manager) shuffledActiveFeeds(fn func(*feed) bool) {
	var active []*feed
	for _, s := range m.sources {
		if s.active {
			active = append(active, s.feeds...)
		}
	}
	m.rnd.Shuffle(len(active), func(i, j int) {
		active[i], active[j] = active[j], active[i]
	})
	for _, f := range active {
		if !fn(f) {
			return
		}
	}
}

func (m *Manager) allFeeds(fn func(*feed) bool) {
	for _, s := range m.sources {
		for _, f := range s.feeds {
			if !fn(f) {
				return
			}
		}
	}
}

func (m *Manager) findFeedByID(feedID int64) *feed {
	for _, s := range m.sources {
		if idx := slices.IndexFunc(s.feeds, func(f *feed) bool { return f.id == feedID }); idx >= 0 {
			return s.feeds[idx]
		}
	}
	return nil
}

func (m *Manager) findSourceByID(sourceID int64) *source {
	if idx := slices.IndexFunc(m.sources, func(s *source) bool { return s.id == sourceID }); idx >= 0 {
		return m.sources[idx]
	}
	return nil
}

func (m *Manager) findSourceByName(name string) *source {
	if idx := slices.IndexFunc(m.sources, func(s *source) bool { return s.name == name }); idx >= 0 {
		return m.sources[idx]
	}
	return nil
}

// refreshFeeds checks if there are feeds that need reloading
// and does so in that case.
func (m *Manager) refreshFeeds() {
	now := time.Now()
	m.activeFeeds(func(f *feed) bool {
		// Does the feed need a refresh?
		if f.nextCheck.IsZero() || !now.Before(f.nextCheck) {
			slog.Debug("refreshing feed", "feed", f.id, "source", f.source.name)
			if err := f.refresh(m); err != nil {
				f.log(m, config.ErrorFeedLogLevel, "feed refresh failed: %v", err.Error())
			}
			// Even if there was an error try again later.
			f.nextCheck = time.Now().Add(m.cfg.Sources.FeedRefresh)
		}
		return true
	})
}

// startDownloads starts downloads if there are enough slots and
// there are things to download.
func (m *Manager) startDownloads() {
	for m.usedSlots < m.cfg.Sources.DownloadSlots {
		started := false
		m.shuffledActiveFeeds(func(f *feed) bool {
			// Has this feed a free slot?
			maxSlots := min(m.cfg.Sources.MaxSlotsPerSource, m.cfg.Sources.DownloadSlots)
			if f.source.slots != nil {
				maxSlots = min(maxSlots, *f.source.slots)
			}
			if f.source.usedSlots >= maxSlots {
				return true
			}
			// Find a candidate to download.
			loc := f.findWaiting()
			if loc == nil {
				return true
			}
			m.usedSlots++
			f.source.usedSlots++
			loc.state = running
			loc.id = m.generateID()
			started = true
			m.jobs <- downloadJob{l: *loc, f: f}
			return m.usedSlots < m.cfg.Sources.DownloadSlots
		})
		if !started {
			return
		}
	}
}

func (dj *downloadJob) finish(m *Manager) {
	m.fns <- func(m *Manager) {
		dj.f.source.usedSlots = max(0, dj.f.source.usedSlots-1)
		m.usedSlots = max(0, m.usedSlots-1)
		if l := dj.f.findLocationByID(dj.l.id); l != nil {
			l.state = done
		}
	}
}

func (m *Manager) download(wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range m.jobs {
		job.l.download(m, job.f)
		job.finish(m)
	}
}

// compactDone removes the locations the feeds which are downloaded.
func (m *Manager) compactDone() {
	m.allFeeds(func(f *feed) bool {
		f.queue = slices.DeleteFunc(f.queue, func(l location) bool {
			return l.state == done
		})
		return true
	})
}

func (m *Manager) generateID() int64 {
	// Start with 1 to avoid clashes with zeroed locations.
	m.uniqueID++
	return m.uniqueID
}

// Run runs the manager. To be used in a Go routine.
func (m *Manager) Run(ctx context.Context) {

	var wg sync.WaitGroup

	for range m.cfg.Sources.DownloadSlots {
		wg.Add(1)
		go m.download(&wg)
	}

	refreshTicker := time.NewTicker(refreshDuration)
	defer refreshTicker.Stop()
out:
	for !m.done {
		m.pmdCache.Cleanup()
		m.keysCache.Cleanup()
		m.compactDone()
		m.refreshFeeds()
		m.startDownloads()
		select {
		case fn := <-m.fns:
			fn(m)
		case <-ctx.Done():
			break out
		case <-refreshTicker.C:
		}
	}
	close(m.jobs)
	wg.Wait()
}

// Source returns infos about a source.
func (m *Manager) Source(id int64, stats bool) *SourceInfo {
	siCh := make(chan *SourceInfo)
	m.fns <- func(m *Manager) {
		s := m.findSourceByID(id)
		if s == nil {
			siCh <- nil
			return
		}
		var st *Stats
		if stats {
			st = new(Stats)
			s.addStats(st)
		}
		siCh <- &SourceInfo{
			ID:                      s.id,
			Name:                    s.name,
			URL:                     s.url,
			Active:                  s.active,
			Status:                  s.status,
			Rate:                    s.rate,
			Slots:                   s.slots,
			Headers:                 s.headers,
			StrictMode:              s.strictMode,
			Insecure:                s.insecure,
			SignatureCheck:          s.signatureCheck,
			Age:                     s.age,
			IgnorePatterns:          s.ignorePatterns,
			HasClientCertPublic:     s.clientCertPublic != nil,
			HasClientCertPrivate:    s.clientCertPrivate != nil,
			HasClientCertPassphrase: s.clientCertPassphrase != nil,
			Stats:                   st,
		}
	}
	return <-siCh
}

// Sources iterates over all sources and passes infos to a given function.
func (m *Manager) Sources(fn func(*SourceInfo), stats bool) {
	done := make(chan struct{})
	m.fns <- func(m *Manager) {
		defer close(done)
		si := new(SourceInfo)
		for _, s := range m.sources {
			var st *Stats
			if stats {
				st = new(Stats)
				s.addStats(st)
			}
			*si = SourceInfo{
				ID:                      s.id,
				Name:                    s.name,
				URL:                     s.url,
				Active:                  s.active,
				Rate:                    s.rate,
				Slots:                   s.slots,
				Headers:                 s.headers,
				StrictMode:              s.strictMode,
				Insecure:                s.insecure,
				SignatureCheck:          s.signatureCheck,
				Age:                     s.age,
				IgnorePatterns:          s.ignorePatterns,
				HasClientCertPublic:     s.clientCertPublic != nil,
				HasClientCertPrivate:    s.clientCertPrivate != nil,
				HasClientCertPassphrase: s.clientCertPassphrase != nil,
				Stats:                   st,
			}
			fn(si)
		}
	}
	<-done
}

// Feeds passes the fields of the feeds of a given source to a given function.
func (m *Manager) Feeds(sourceID int64, fn func(*FeedInfo), stats bool) error {
	errCh := make(chan error)
	m.fns <- func(m *Manager) {
		s := m.findSourceByID(sourceID)
		if s == nil {
			errCh <- NoSuchEntryError("no such source")
			return
		}
		fi := new(FeedInfo)
		for _, f := range s.feeds {
			if f.invalid.Load() {
				continue
			}
			var st *Stats
			if stats {
				st = new(Stats)
				f.addStats(st)
			}
			*fi = FeedInfo{
				ID:    f.id,
				Label: f.label,
				URL:   f.url,
				Rolie: f.rolie,
				Lvl:   config.FeedLogLevel(f.logLevel.Load()),
				Stats: st,
			}
			fn(fi)
		}
		errCh <- nil
	}
	return <-errCh
}

// Feed returns the infos of a feed.
func (m *Manager) Feed(feedID int64, stats bool) *FeedInfo {
	fiCh := make(chan *FeedInfo)
	m.fns <- func(m *Manager) {
		f := m.findFeedByID(feedID)
		if f == nil || f.invalid.Load() {
			fiCh <- nil
			return
		}
		var st *Stats
		if stats {
			st = new(Stats)
			f.addStats(st)
		}
		fiCh <- &FeedInfo{
			ID:    f.id,
			Label: f.label,
			URL:   f.url,
			Rolie: f.rolie,
			Lvl:   config.FeedLogLevel(f.logLevel.Load()),
			Stats: st,
		}
	}
	return <-fiCh
}

// FeedLog sends the log of the feed with the given id to the given function.
func (m *Manager) FeedLog(
	feedID int64,
	fn func(
		t time.Time,
		lvl config.FeedLogLevel,
		msg string),
	limit, offset int64,
	logLevels []config.FeedLogLevel,
	count bool,
) (int64, error) {
	const (
		countSQL  = `SELECT count(*) FROM feed_logs WHERE `
		selectSQL = `SELECT time, lvl::text, msg FROM feed_logs WHERE `
	)

	var cond strings.Builder
	cond.WriteString(`feeds_id = $1`)

	args := []any{feedID}
	if len(logLevels) > 0 {
		cond.WriteString(` AND (`)
		for i, lvl := range logLevels {
			if i > 0 {
				cond.WriteString(` OR `)
			}
			cond.WriteString(`lvl = $`)
			cond.WriteString(strconv.Itoa(len(args) + 1))
			args = append(args, lvl)
		}
		cond.WriteByte(')')
	}

	var cntSQL string
	var cntArgs []any

	if count {
		// Counting ignores limit, offset and order.
		cntSQL = countSQL + cond.String()
		cntArgs = args
		slog.Debug("feed log count", "stmt", cntSQL)
	}

	cond.WriteString(` ORDER by time DESC`)

	if offset >= 0 {
		cond.WriteString(` OFFSET $`)
		cond.WriteString(strconv.Itoa(len(args) + 1))
		args = append(args, offset)
	}
	if limit >= 0 {
		cond.WriteString(` LIMIT $`)
		cond.WriteString(strconv.Itoa(len(args) + 1))
		args = append(args, limit)
	}

	selSQL := selectSQL + cond.String()
	slog.Debug("feed log select", "stmt", selSQL)

	counter := int64(-1)

	err := m.db.Run(
		context.Background(),
		func(ctx context.Context, con *pgxpool.Conn) error {
			if count {
				if err := con.QueryRow(ctx, cntSQL, cntArgs...).Scan(&counter); err != nil {
					return fmt.Errorf("counting feed logs failed: %w", err)
				}
			}
			rows, err := con.Query(ctx, selSQL, args...)
			if err != nil {
				return fmt.Errorf("querying feed logs failed: %w", err)
			}
			defer rows.Close()
			var (
				t   time.Time
				lvl config.FeedLogLevel
				msg string
			)
			for rows.Next() {
				if err := rows.Scan(&t, &lvl, &msg); err != nil {
					return fmt.Errorf("scanning log failed: %w", err)
				}
				fn(t, lvl, msg)
			}
			return rows.Err()
		}, 0,
	)
	return counter, err
}

// ping wakes up the manager.
func (m *Manager) ping() {}

func (m *Manager) backgroundPing() {
	go func() { m.fns <- (*Manager).ping }()
}

// Kill stops the manager.
func (m *Manager) Kill() {
	m.fns <- func(m *Manager) { m.done = true }
}

func (m *Manager) removeSource(sourceID int64) error {
	if m.findSourceByID(sourceID) == nil {
		return NoSuchEntryError("no such source")
	}
	const sql = `DELETE FROM sources WHERE id = $1`
	notFound := false
	if err := m.db.Run(
		context.Background(),
		func(rctx context.Context, con *pgxpool.Conn) error {
			tags, err := con.Exec(rctx, sql, sourceID)
			if err != nil {
				return fmt.Errorf("removing source failed: %w", err)
			}
			notFound = tags.RowsAffected() == 0
			return nil
		}, 0,
	); err != nil {
		return fmt.Errorf("deleting source from db failed: %w", err)
	}
	m.sources = slices.DeleteFunc(m.sources, func(s *source) bool {
		if s.id == sourceID {
			s.active = false
			s.feeds = nil
			return true
		}
		return false
	})
	// XXX: Should not happen!
	if notFound {
		return NoSuchEntryError("no such source")
	}
	return nil
}

func (m *Manager) removeFeed(feedID int64) error {
	f := m.findFeedByID(feedID)
	if f == nil {
		return NoSuchEntryError("no such feed")
	}
	f.invalid.Store(true)
	const sql = `DELETE FROM feeds WHERE id = $1`
	if err := m.db.Run(
		context.Background(),
		func(ctx context.Context, con *pgxpool.Conn) error {
			_, err := con.Exec(ctx, sql, feedID)
			return err
		}, 0,
	); err != nil {
		return fmt.Errorf("deleting feed failed: %w", err)
	}
	s := f.source
	s.feeds = slices.DeleteFunc(s.feeds, func(g *feed) bool { return f == g })
	return nil
}

func (m *Manager) inManager(fn func(*Manager)) {
	done := make(chan struct{})
	m.fns <- func(m *Manager) {
		defer close(done)
		fn(m)
	}
	<-done
}

func (m *Manager) asManager(fn func(*Manager, int64) error, id int64) error {
	err := make(chan error)
	m.fns <- func(m *Manager) { err <- fn(m, id) }
	return <-err
}

// AddSource registers a new source.
func (m *Manager) AddSource(
	name string,
	url string,
	rate *float64,
	slots *int,
	headers []string,
	strictMode *bool,
	insecure *bool,
	signatureCheck *bool,
	age *time.Duration,
	ignorePatterns []*regexp.Regexp,
	clientCertPublic []byte,
	clientCertPrivate []byte,
	clientCertPassphrase []byte,
) (int64, error) {
	lpmd := m.PMD(url)
	if !lpmd.Valid() {
		return 0, InvalidArgumentError("PMD is invalid")
	}
	errCh := make(chan error)
	s := &source{
		name:                 name,
		url:                  url,
		rate:                 rate,
		slots:                slots,
		headers:              headers,
		strictMode:           strictMode,
		insecure:             insecure,
		signatureCheck:       signatureCheck,
		age:                  age,
		ignorePatterns:       ignorePatterns,
		clientCertPublic:     clientCertPublic,
		clientCertPrivate:    clientCertPrivate,
		clientCertPassphrase: clientCertPassphrase,
	}
	if clientCertPrivate != nil {
		var err error
		if clientCertPrivate, err = m.encrypt(clientCertPrivate); err != nil {
			return 0, err
		}
	}
	if clientCertPassphrase != nil {
		var err error
		if clientCertPassphrase, err = m.encrypt(clientCertPassphrase); err != nil {
			return 0, err
		}
	}
	m.fns <- func(m *Manager) {
		if m.findSourceByName(name) != nil {
			errCh <- InvalidArgumentError("source already exists")
			return
		}
		const sql = `INSERT INTO sources (` +
			`name, url, rate, slots, headers, ` +
			`strict_mode, insecure, signature_check, age, ignore_patterns, ` +
			`client_cert_public, client_cert_private, client_cert_passphrase) ` +
			`VALUES (` +
			`$1, $2, $3, $4, $5, ` +
			`$6, $7, $8, $9, $10, ` +
			`$11, $12, $13) ` +
			`RETURNING id`
		if err := m.db.Run(
			context.Background(),
			func(rctx context.Context, con *pgxpool.Conn) error {
				return con.QueryRow(rctx, sql,
					name, url, rate, slots, headers,
					strictMode, insecure, signatureCheck, age, ignorePatterns,
					clientCertPublic, clientCertPrivate, clientCertPassphrase,
				).Scan(&s.id)
			}, 0,
		); err != nil {
			errCh <- fmt.Errorf("adding source to database failed: %w", err)
			return
		}
		m.sources = append(m.sources, s)
		errCh <- nil
	}
	return s.id, <-errCh
}

// AddFeed adds a new feed to a source.
func (m *Manager) AddFeed(
	sourceID int64,
	label string,
	url *url.URL,
	logLevel config.FeedLogLevel,
) (int64, error) {
	var feedID int64
	errCh := make(chan error)
	m.fns <- func(m *Manager) {
		s := m.findSourceByID(sourceID)
		if s == nil {
			errCh <- NoSuchEntryError("no such source")
			return
		}
		if slices.ContainsFunc(s.feeds, func(f *feed) bool { return f.label == label }) {
			errCh <- InvalidArgumentError("label already exists")
			return
		}
		pmd, err := asProviderMetaData(m.PMD(s.url))
		if err != nil {
			errCh <- err
			return
		}
		rolie := isROLIEFeed(pmd, url.String())
		if !rolie && !isDirectoryFeed(pmd, url.String()) {
			errCh <- InvalidArgumentError("feed is neither ROLIE nor directory based")
			return
		}
		const sql = `INSERT INTO feeds (label, sources_id, url, rolie, log_lvl) ` +
			`VALUES ($1, $2, $3, $4, $5::feed_logs_level) ` +
			`RETURNING id`
		if err := m.db.Run(
			context.Background(),
			func(ctx context.Context, conn *pgxpool.Conn) error {
				return conn.QueryRow(ctx, sql,
					label,
					sourceID,
					url.String(),
					rolie,
					logLevel,
				).Scan(&feedID)
			}, 0,
		); err != nil {
			errCh <- fmt.Errorf("inserting feed failed: %w", err)
			return
		}
		f := &feed{
			id:     feedID,
			label:  label,
			url:    url,
			rolie:  rolie,
			source: s,
		}
		f.logLevel.Store(int32(logLevel))
		s.feeds = append(s.feeds, f)
		if s.active {
			m.backgroundPing()
		}
		errCh <- nil
	}
	if err := <-errCh; err != nil {
		return 0, err
	}
	return feedID, nil
}

// RemoveSource removes a sources from manager.
func (m *Manager) RemoveSource(sourceID int64) error {
	return m.asManager((*Manager).removeSource, sourceID)
}

// RemoveFeed removes a feed from a source.
func (m *Manager) RemoveFeed(feedID int64) error {
	return m.asManager((*Manager).removeFeed, feedID)
}

// PMD returns the provider metadata from the given url.
func (m *Manager) PMD(url string) *csaf.LoadedProviderMetadata {
	return m.pmdCache.pmd(m, url)
}

// updater collects updates so that only the first update on
// a field is done, only updates which change things are
// registered and applies the updates only in case that persisting
// them first has worked.
type updater[T any] struct {
	updatable T
	manager   *Manager
	changes   []func(T)
	fields    []string
	values    []any
}

func (u *updater[T]) addChange(ch func(T), field string, value any) {
	if !slices.Contains(u.fields, field) {
		u.changes = append(u.changes, ch)
		u.fields = append(u.fields, field)
		u.values = append(u.values, value)
	}
}

func (u *updater[T]) applyChanges() bool {
	for _, ch := range u.changes {
		if ch != nil {
			ch(u.updatable)
		}
	}
	return len(u.changes) > 0
}

func (u *updater[T]) updateDB(table string, id int64) error {
	if len(u.fields) == 0 {
		return nil
	}
	var ob, cb string
	if len(u.fields) > 1 {
		ob, cb = "(", ")"
	}
	sql := fmt.Sprintf(
		"UPDATE %[6]s SET %[1]s%[3]s%[2]s = %[1]s%[4]s%[2]s WHERE id = %[5]d",
		ob, cb,
		strings.Join(u.fields, ","),
		placeholders(len(u.values)),
		id, table)
	return u.manager.db.Run(
		context.Background(),
		func(ctx context.Context, conn *pgxpool.Conn) error {
			_, err := conn.Exec(ctx, sql, u.values...)
			return err
		}, 0)
}

func placeholders(n int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(i + 1))
	}
	return b.String()
}

// SourceUpdater offers a protocol to update a source. Call the UpdateX
// (with X in Name, Rate, ...) methods to update specfic fields.
type SourceUpdater struct {
	updater[*source]
	clientCertUpdated bool
}

// UpdateName requests a name update.
func (su *SourceUpdater) UpdateName(name string) error {
	if name == su.updatable.name {
		return nil
	}
	if name == "" || su.manager.findSourceByName(name) != nil {
		return InvalidArgumentError("invalid name")
	}
	su.addChange(func(s *source) { s.name = name }, "name", name)
	return nil
}

// UpdateRate requests a rate update.
func (su *SourceUpdater) UpdateRate(rate *float64) error {
	if rate == nil && su.updatable.rate == nil {
		return nil
	}
	if rate != nil && su.updatable.rate != nil && *rate == *su.updatable.rate {
		return nil
	}
	if rate != nil && (*rate <= 0 ||
		(*rate > su.manager.cfg.Sources.MaxRatePerSource && su.manager.cfg.Sources.MaxRatePerSource != 0)) {
		return InvalidArgumentError("rate value out of range")
	}
	su.addChange(func(s *source) { s.setRate(rate) }, "rate", rate)
	return nil
}

// UpdateSlots requests a slots update.
func (su *SourceUpdater) UpdateSlots(slots *int) error {
	if slots == nil && su.updatable.slots == nil {
		return nil
	}
	if slots != nil && su.updatable.slots != nil && *slots == *su.updatable.slots {
		return nil
	}
	if slots != nil && (*slots < 1 ||
		(*slots > su.manager.cfg.Sources.MaxSlotsPerSource && su.manager.cfg.Sources.MaxSlotsPerSource != 0)) {
		return InvalidArgumentError("slot value ot ouf range")
	}
	su.addChange(func(s *source) { s.slots = slots }, "slots", slots)
	return nil
}

// UpdateActive requests an active update.
func (su *SourceUpdater) UpdateActive(active bool) error {
	if active == su.updatable.active {
		return nil
	}
	su.addChange(func(s *source) {
		s.active = active
		s.status = nil
		if active {
			su.manager.backgroundPing()
		}
	}, "active", active)
	return nil
}

// clone as slices.Clone sadly does not work that way.
func clone[S ~[]E, E any](s S) S {
	if len(s) == 0 {
		return nil
	}
	return append(make(S, 0, len(s)), s...)
}

// UpdateHeaders requests a headers update.
func (su *SourceUpdater) UpdateHeaders(headers []string) error {
	if slices.Equal(headers, su.updatable.headers) {
		return nil
	}
	headers = clone(headers)
	su.addChange(func(s *source) { s.headers = headers }, "headers", headers)
	return nil
}

// UpdateStrictMode requests an update on strictMode.
func (su *SourceUpdater) UpdateStrictMode(strictMode *bool) error {
	if su.updatable.strictMode == nil && strictMode == nil {
		return nil
	}
	if su.updatable.strictMode != nil && strictMode != nil && *su.updatable.strictMode == *strictMode {
		return nil
	}
	su.addChange(func(s *source) { s.strictMode = strictMode }, "strict_mode", strictMode)
	return nil
}

// UpdateInsecure requests an update on insecure.
func (su *SourceUpdater) UpdateInsecure(insecure *bool) error {
	if su.updatable.insecure == nil && insecure == nil {
		return nil
	}
	if su.updatable.insecure != nil && insecure != nil && *su.updatable.insecure == *insecure {
		return nil
	}
	su.addChange(func(s *source) { s.insecure = insecure }, "insecure", insecure)
	return nil
}

// UpdateSignatureCheck requests an update on signatureCheck.
func (su *SourceUpdater) UpdateSignatureCheck(signatureCheck *bool) error {
	if su.updatable.signatureCheck == nil && signatureCheck == nil {
		return nil
	}
	if su.updatable.signatureCheck != nil && signatureCheck != nil && *su.updatable.signatureCheck == *signatureCheck {
		return nil
	}
	su.addChange(func(s *source) { s.signatureCheck = signatureCheck }, "signature_check", signatureCheck)
	return nil
}

// UpdateAge requests an update on age.
func (su *SourceUpdater) UpdateAge(age *time.Duration) error {
	if su.updatable.age == nil && age == nil {
		return nil
	}
	if su.updatable.age != nil && age != nil && *su.updatable.age == *age {
		return nil
	}
	if age != nil && (*age > su.manager.cfg.Sources.MaxAge && su.manager.cfg.Sources.MaxAge != 0) {
		return InvalidArgumentError("invalid age value")
	}
	su.addChange(func(s *source) { s.setAge(age) }, "age", age)
	return nil
}

// UpdateIgnorePatterns requests an update on ignorepatterns.
func (su *SourceUpdater) UpdateIgnorePatterns(ignorePatterns []*regexp.Regexp) error {
	if slices.EqualFunc(su.updatable.ignorePatterns, ignorePatterns,
		func(a, b *regexp.Regexp) bool { return a != nil && b != nil && a.String() == b.String() }) {
		return nil
	}
	ignorePatterns = clone(ignorePatterns)
	su.addChange(func(s *source) { s.setIgnorePatterns(ignorePatterns) }, "ignore_patterns", ignorePatterns)
	return nil
}

// UpdateClientCertPublic requests an update ob client cert public part.
func (su *SourceUpdater) UpdateClientCertPublic(data []byte) error {
	if data == nil && su.updatable.clientCertPublic == nil {
		return nil
	}
	if data != nil && su.updatable.clientCertPublic != nil && slices.Equal(data, su.updatable.clientCertPublic) {
		return nil
	}
	data = clone(data)
	su.addChange(func(s *source) {
		su.clientCertUpdated = true
		s.clientCertPublic = data
	}, "client_cert_public", data)
	return nil
}

// UpdateClientCertPrivate requests an update ob client cert private part.
func (su *SourceUpdater) UpdateClientCertPrivate(data []byte) error {
	orig := su.updatable.clientCertPrivate
	if data == nil && orig == nil {
		return nil
	}
	if data != nil && orig != nil && slices.Equal(data, orig) {
		return nil
	}
	encrypted, err := su.manager.encrypt(data)
	if err != nil {
		return err
	}
	data = clone(data)
	su.addChange(func(s *source) {
		su.clientCertUpdated = true
		s.clientCertPrivate = data
	}, "client_cert_private", encrypted)
	return nil
}

// UpdateClientCertPassphrase requests an update ob client cert private part.
func (su *SourceUpdater) UpdateClientCertPassphrase(data []byte) error {
	orig := su.updatable.clientCertPassphrase
	if data == nil && orig == nil {
		return nil
	}
	if data != nil && orig != nil && slices.Equal(data, orig) {
		return nil
	}
	encrypted, err := su.manager.encrypt(data)
	if err != nil {
		return err
	}
	data = clone(data)
	su.addChange(func(s *source) {
		su.clientCertUpdated = true
		s.clientCertPassphrase = data
	}, "client_cert_passphrase", encrypted)
	return nil
}

// UpdateSource passes an updater to manipulate a source with a given id to a given callback.
func (m *Manager) UpdateSource(
	sourceID int64,
	updates func(*SourceUpdater) error,
) (SourceUpdateResult, error) {
	type result struct {
		v   SourceUpdateResult
		err error
	}
	resCh := make(chan result)
	m.fns <- func(m *Manager) {
		s := m.findSourceByID(sourceID)
		if s == nil {
			resCh <- result{err: NoSuchEntryError("no such source")}
			return
		}
		su := SourceUpdater{updater: updater[*source]{updatable: s, manager: m}}
		if err := updates(&su); err != nil {
			resCh <- result{err: fmt.Errorf("updates failed: %w", err)}
			return
		}
		if err := su.updateDB("sources", s.id); err != nil {
			resCh <- result{err: fmt.Errorf("updating database failed: %w", err)}
			return
		}
		// Only apply changes if database updates went through.
		if !su.applyChanges() {
			resCh <- result{v: SourceUnchanged}
			return
		}
		if su.clientCertUpdated {
			if err := s.updateCertificate(); err != nil {
				slog.Warn("updating client cert failed", "warn", err)
				if s.active {
					s.active = false
					s.status = []string{deactivatedDueToClientCertIssue}
					x := SourceUpdater{updater: updater[*source]{updatable: s, manager: m}}
					x.addChange(nil, "active", false)
					if err := x.updateDB("sources", s.id); err != nil {
						slog.Error("deactivating source failed", "err", err)
					}
					resCh <- result{v: SourceDeactivated}
					return
				}
			} else {
				s.status = nil
			}
		}
		resCh <- result{v: SourceUpdated}
	}
	res := <-resCh
	return res.v, res.err
}

// FeedUpdater offers a protocol to update a source. Call the UpdateX
// (with X in LogLevel, Label) methods to update specfic fields.
type FeedUpdater struct {
	updater[*feed]
}

// UpdateLogLevel requests an update on the log level of the feed.
func (fu *FeedUpdater) UpdateLogLevel(level config.FeedLogLevel) error {
	if config.FeedLogLevel(fu.updatable.logLevel.Load()) == level {
		return nil
	}
	fu.addChange(func(f *feed) { f.logLevel.Store(int32(level)) }, "log_lvl", level)
	return nil
}

// UpdateLabel requests an update on the label of the feed.
func (fu *FeedUpdater) UpdateLabel(label string) error {
	if fu.updatable.label == label {
		return nil
	}
	if label == "" || slices.ContainsFunc(fu.updatable.source.feeds, func(f *feed) bool {
		return f.label == label
	}) {
		return InvalidArgumentError("invalid label")
	}
	fu.addChange(func(f *feed) { f.label = label }, "label", label)
	return nil
}

// UpdateFeed passes an updater to manipulate a feed with a given id to a given callback.
func (m *Manager) UpdateFeed(
	feedID int64,
	updates func(*FeedUpdater) error,
) (bool, error) {
	type result struct {
		updated bool
		err     error
	}
	resCh := make(chan result)
	m.fns <- func(m *Manager) {
		f := m.findFeedByID(feedID)
		if f == nil {
			resCh <- result{err: NoSuchEntryError("no such feed")}
			return
		}
		fu := FeedUpdater{updater: updater[*feed]{updatable: f, manager: m}}
		if err := updates(&fu); err != nil {
			resCh <- result{err: fmt.Errorf("updates failed: %w", err)}
			return
		}
		if err := fu.updateDB("feeds", f.id); err != nil {
			resCh <- result{err: fmt.Errorf("updating database failed: %w", err)}
			return
		}
		// Only apply changes if database updates went through.
		resCh <- result{updated: fu.applyChanges()}
	}
	res := <-resCh
	return res.updated, res.err
}
