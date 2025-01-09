// This file is Free Software under the Apache-2.0 License
// without warranty, see README.md and LICENSES/Apache-2.0.txt for details.
//
// SPDX-License-Identifier: Apache-2.0
//
// SPDX-FileCopyrightText: 2024 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
// Software-Engineering: 2024 Intevation GmbH <https://intevation.de>

package web

import (
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ISDuBA/ISDuBA/pkg/config"
	"github.com/ISDuBA/ISDuBA/pkg/models"
	"github.com/ISDuBA/ISDuBA/pkg/sources"
	"github.com/gin-gonic/gin"
)

type sourceAge struct {
	time.Duration
}

// UnmarshalParam implements [binding.BindUnmarshaler].
func (sa *sourceAge) UnmarshalParam(param string) error {
	duration, err := time.ParseDuration(param)
	if err != nil {
		return err
	}
	*sa = sourceAge{duration}
	return nil
}

// MarshalText implements [encoding.TextMarshaler].
func (sa sourceAge) MarshalText() ([]byte, error) {
	s := sa.String()
	return []byte(s), nil
}

type source struct {
	ID                   int64          `json:"id" form:"id"`
	Name                 string         `json:"name" form:"name" binding:"required,min=1"`
	URL                  string         `json:"url" form:"url" binding:"required,min=1"`
	Active               bool           `json:"active" form:"active"`
	Attention            bool           `json:"attention" form:"attention"`
	Status               []string       `json:"status,omitempty"`
	Rate                 *float64       `json:"rate,omitempty" form:"rate" binding:"omitnil,gte=0"`
	Slots                *int           `json:"slots,omitempty" form:"slots" binding:"omitnil,gte=0"`
	Headers              []string       `json:"headers,omitempty" form:"headers"`
	StrictMode           *bool          `json:"strict_mode,omitempty" form:"strict_mode"`
	Secure               *bool          `json:"secure,omitempty" form:"secure"`
	SignatureCheck       *bool          `json:"signature_check,omitempty" form:"signature_check"`
	Age                  *sourceAge     `json:"age,omitempty" form:"age"`
	IgnorePatterns       []string       `json:"ignore_patterns,omitempty" form:"ignore_patterns"`
	ClientCertPublic     *string        `json:"client_cert_public,omitempty" form:"client_cert_public"`
	ClientCertPrivate    *string        `json:"client_cert_private,omitempty" form:"client_cert_private"`
	ClientCertPassphrase *string        `json:"client_cert_passphrase,omitempty" form:"client_cert_passphrase"`
	Stats                *sources.Stats `json:"stats,omitempty"`
}

type feed struct {
	ID       int64               `json:"id"`
	Label    string              `json:"label"`
	URL      string              `json:"url"`
	Rolie    bool                `json:"rolie"`
	LogLevel config.FeedLogLevel `json:"log_level"`
	Stats    *sources.Stats      `json:"stats,omitempty"`
}

var stars = "***"

func threeStars(b bool) *string {
	if b {
		return &stars
	}
	return nil
}

func newSource(si *sources.SourceInfo) *source {
	var sa *sourceAge
	if si.Age != nil {
		sa = &sourceAge{*si.Age}
	}
	return &source{
		ID:                   si.ID,
		Name:                 si.Name,
		URL:                  si.URL,
		Active:               si.Active,
		Attention:            si.Attention,
		Status:               si.Status,
		Rate:                 si.Rate,
		Slots:                si.Slots,
		Headers:              si.Headers,
		StrictMode:           si.StrictMode,
		Secure:               si.Secure,
		SignatureCheck:       si.SignatureCheck,
		Age:                  sa,
		IgnorePatterns:       sources.AsStrings(si.IgnorePatterns),
		ClientCertPublic:     threeStars(si.HasClientCertPublic),
		ClientCertPrivate:    threeStars(si.HasClientCertPrivate),
		ClientCertPassphrase: threeStars(si.HasClientCertPassphrase),
		Stats:                si.Stats,
	}
}

func newFeed(fi *sources.FeedInfo) *feed {
	return &feed{
		ID:       fi.ID,
		Label:    fi.Label,
		URL:      fi.URL.String(),
		Rolie:    fi.Rolie,
		LogLevel: fi.Lvl,
		Stats:    fi.Stats,
	}
}

func showStats(ctx *gin.Context) (bool, bool) {
	st := ctx.Query("stats")
	if st == "" {
		return false, true
	}
	return parse(ctx, strconv.ParseBool, st)
}

// viewSources is an endpoint that returns information about the source.
//
//	@Summary		Return all sources.
//	@Description	Returns the source configuration and metadata of all sources.
//	@Param			stats	query	bool	false	"Enable statistic"
//	@Produce		json
//	@Success		200	{object}	web.viewSources.sourcesResult
//	@Failure		400	{object}	models.Error	"could not parse stats"
//	@Router			/sources [get]
func (c *Controller) viewSources(ctx *gin.Context) {
	stats, ok := showStats(ctx)
	if !ok {
		models.SendErrorMessage(ctx, http.StatusBadRequest, "could not parse stats")
		return
	}
	type sourcesResult struct {
		Sources []*source `json:"sources"`
	}
	srcs := []*source{}
	c.sm.Sources(func(si *sources.SourceInfo) {
		srcs = append(srcs, newSource(si))
	}, stats)
	ctx.JSON(http.StatusOK, sourcesResult{Sources: srcs})
}

// hasBlock checks if input has a PEM block.
func hasBlock(data []byte) bool {
	block, _ := pem.Decode(data)
	return block != nil
}

// createSource is an endpoint that creates a source.
//
//	@Summary		Creates a source.
//	@Description	Creates a source with the specified configuration.
//	@Param			url	formData	source	true	"source configuration"
//	@Accept			multipart/form-data
//	@Produce		json
//	@Success		201	{array}		models.ID
//	@Failure		400	{object}	models.Error
//	@Failure		500	{object}	models.Error
//	@Router			/sources [post]
func (c *Controller) createSource(ctx *gin.Context) {
	var src source
	if err := ctx.ShouldBind(&src); err != nil {
		models.SendError(ctx, http.StatusBadRequest, err)
		return
	}
	if src.Rate != nil &&
		(c.cfg.Sources.MaxRatePerSource != 0 && *src.Rate > c.cfg.Sources.MaxRatePerSource) {

		ctx.JSON(http.StatusBadRequest, gin.H{"error": "'rate' out of range"})
		return
	}
	if src.Rate != nil && *src.Rate == 0 {
		src.Rate = nil
	}
	if src.Slots != nil && *src.Slots > c.cfg.Sources.MaxSlotsPerSource {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "'slots' out of range"})
		return
	}
	if src.Slots != nil && *src.Slots == 0 {
		src.Slots = nil
	}
	if err := validateHeaders(src.Headers); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ignorePatterns, err := sources.AsRegexps(src.IgnorePatterns)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var clientCertPublic, clientCertPrivate, clientCertPassphrase []byte
	if src.ClientCertPublic != nil {
		clientCertPublic = []byte(*src.ClientCertPublic)
		if !hasBlock(clientCertPublic) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "client_cert_public has no PEM block"})
			return
		}
	}
	if src.ClientCertPrivate != nil {
		clientCertPrivate = []byte(*src.ClientCertPrivate)
		if !hasBlock(clientCertPrivate) {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "client_cert_private has no PEM block"})
			return
		}
	}
	if src.ClientCertPassphrase != nil {
		clientCertPassphrase = []byte(*src.ClientCertPassphrase)
	}

	var age *time.Duration
	if src.Age != nil {
		age = &src.Age.Duration
	}
	if src.Age == nil && c.cfg.Sources.DefaultAge != 0 {
		age = &c.cfg.Sources.DefaultAge
	}

	switch id, err := c.sm.AddSource(
		src.Name,
		src.URL,
		src.Rate,
		src.Slots,
		src.Headers,
		src.StrictMode,
		src.Secure,
		src.SignatureCheck,
		age,
		ignorePatterns,
		clientCertPublic,
		clientCertPrivate,
		clientCertPassphrase,
	); {
	case err == nil:
		ctx.JSON(http.StatusCreated, models.ID{ID: id})
	case errors.Is(err, sources.InvalidArgumentError("")):
		models.SendError(ctx, http.StatusBadRequest, err)
	default:
		slog.Error("database error", "err", err)
		models.SendError(ctx, http.StatusInternalServerError, err)
	}
}

// deleteSource is an endpoint that deletes the source with specified id.
//
//	@Summary		Deletes a source.
//	@Description	Deletes the source configuration with the specified id.
//	@Param			id	path	int	true	"Source ID"
//	@Produce		json
//	@Success		200	{object}	models.Success	"source deleted"
//	@Failure		400	{object}	models.Error
//	@Failure		404	{object}	models.Error
//	@Failure		500	{object}	models.Error
//	@Router			/sources/{id} [delete]
func (c *Controller) deleteSource(ctx *gin.Context) {
	var input struct {
		ID int64 `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&input); err != nil {
		models.SendError(ctx, http.StatusBadRequest, err)
		return
	}
	switch err := c.sm.RemoveSource(input.ID); {
	case err == nil:
		models.SendSuccess(ctx, http.StatusOK, "source deleted")
	case errors.Is(err, sources.NoSuchEntryError("")):
		models.SendError(ctx, http.StatusNotFound, err)
	default:
		slog.Error("database error", "err", err)
		models.SendError(ctx, http.StatusInternalServerError, err)
	}
}

// viewSource is an endpoint that returns information about the source.
//
//	@Summary		Get source information.
//	@Description	Returns the source configuration and metadata.
//	@Param			id		path	int		true	"Source ID"
//	@Param			stats	query	bool	false	"Enable statistic"
//	@Produce		json
//	@Success		201	{object}	models.Success
//	@Failure		400	{object}	models.Error	"could not parse stats"
//	@Failure		404	{object}	models.Error
//	@Router			/sources/{id} [get]
func (c *Controller) viewSource(ctx *gin.Context) {
	var input struct {
		ID int64 `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	stats, ok := showStats(ctx)
	if !ok {
		models.SendErrorMessage(ctx, http.StatusBadRequest, "could not parse stats")
		return
	}
	si := c.sm.Source(input.ID, stats)
	if si == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx.JSON(http.StatusOK, newSource(si))
}

// updateSource is an endpoint that updates the source configuration.
//
//	@Summary		Updates source configuration.
//	@Description	Updates the source configuration.
//	@Param			id	path	int	true	"Source ID"
//	@Produce		json
//	@Success		201	{object}	models.Success
//	@Failure		400	{object}	models.Error
//	@Failure		404	{object}	models.Error	"not found"
//	@Failure		500	{object}	models.Error
//	@Router			/sources/{id} [put]
func (c *Controller) updateSource(ctx *gin.Context) {
	var input struct {
		SourceID int64 `uri:"id"`
	}
	if err := ctx.ShouldBindUri(&input); err != nil {
		models.SendError(ctx, http.StatusBadRequest, err)
		return
	}
	switch ur, err := c.sm.UpdateSource(input.SourceID, func(su *sources.SourceUpdater) error {
		// name
		if name, ok := ctx.GetPostForm("name"); ok {
			if err := su.UpdateName(name); err != nil {
				return err
			}
		}
		// rate
		if rate, ok := ctx.GetPostForm("rate"); ok {
			var r *float64
			if rate != "" {
				x, err := strconv.ParseFloat(rate, 64)
				if err != nil {
					return sources.InvalidArgumentError(
						fmt.Sprintf("parsing 'rate' failed: %v", err.Error()))
				}
				if x == 0 {
					r = nil
				} else {
					r = &x
				}
			}
			if err := su.UpdateRate(r); err != nil {
				return err
			}
		}
		// slots
		if slots, ok := ctx.GetPostForm("slots"); ok {
			var sl *int
			if slots != "" {
				x, err := strconv.Atoi(slots)
				if err != nil {
					return sources.InvalidArgumentError(
						fmt.Sprintf("parsing 'slots' failed: %v", err.Error()))
				}
				if x == 0 {
					sl = nil
				} else {
					sl = &x
				}
			}
			if err := su.UpdateSlots(sl); err != nil {
				return err
			}
		}
		// active
		if active, ok := ctx.GetPostForm("active"); ok {
			act, err := strconv.ParseBool(active)
			if err != nil {
				return sources.InvalidArgumentError(
					fmt.Sprintf("parsing 'active' failed: %v", err.Error()))
			}
			if err := su.UpdateActive(act); err != nil {
				return err
			}
		}
		// attention
		if attention, ok := ctx.GetPostForm("attention"); ok {
			att, err := strconv.ParseBool(attention)
			if err != nil {
				return sources.InvalidArgumentError(
					fmt.Sprintf("parsing 'attention' failed: %v", err.Error()))
			}
			if err := su.UpdateAttention(att); err != nil {
				return err
			}
		}
		// headers
		if headers, ok := ctx.GetPostFormArray("headers"); ok {
			if err := validateHeaders(headers); err != nil {
				return err
			}
			if err := su.UpdateHeaders(headers); err != nil {
				return err
			}
		} else if err := su.UpdateHeaders([]string{}); err != nil {
			return err
		}

		// Little helper function for the otional bool fields.
		optBool := func(option string, update func(*bool) error) error {
			value, ok := ctx.GetPostForm(option)
			if !ok {
				return nil
			}
			var b *bool
			if value != "" {
				v, err := strconv.ParseBool(value)
				if err != nil {
					return sources.InvalidArgumentError(
						fmt.Sprintf("parsing %q failed: %v", option, err.Error()))
				}
				b = &v
			}
			return update(b)
		}
		// strictMode
		if err := optBool("strict_mode", su.UpdateStrictMode); err != nil {
			return err
		}
		// secure
		if err := optBool("secure", su.UpdateSecure); err != nil {
			return err
		}
		// signatureCheck
		if err := optBool("signature_check", su.UpdateSignatureCheck); err != nil {
			return err
		}
		// age
		if value, ok := ctx.GetPostForm("age"); ok {
			var age *time.Duration
			if value != "" {
				d, err := time.ParseDuration(value)
				if err != nil {
					return sources.InvalidArgumentError(
						fmt.Sprintf("parsing 'age' failed: %v", err.Error()))
				}
				if d != 0 {
					age = &d
				}
			}
			if err := su.UpdateAge(age); err != nil {
				return err
			}
		}
		// ignorePatterns
		if patterns, ok := ctx.GetPostFormArray("ignore_patterns"); ok {
			regexps, err := sources.AsRegexps(patterns)
			if err != nil {
				return err
			}
			if err := su.UpdateIgnorePatterns(regexps); err != nil {
				return err
			}
		}
		// client certificate update
		optCert := func(option string, update func([]byte) error) error {
			cert, ok := ctx.GetPostForm(option)
			if !ok {
				return nil
			}
			var data []byte
			if cert != "" {
				data = []byte(cert)
				if !hasBlock(data) {
					return sources.InvalidArgumentError(
						fmt.Sprintf("%q has no PEM block", option))
				}
			}
			return update(data)
		}
		if err := optCert("client_cert_public", su.UpdateClientCertPublic); err != nil {
			return err
		}
		if err := optCert("client_cert_private", su.UpdateClientCertPrivate); err != nil {
			return err
		}
		if passphrase, ok := ctx.GetPostForm("client_cert_passphrase"); ok {
			var data []byte
			if passphrase != "" {
				data = []byte(passphrase)
			}
			if err := su.UpdateClientCertPassphrase(data); err != nil {
				return err
			}
		}
		return nil
	}); {
	case err == nil:
		models.SendSuccess(ctx, http.StatusOK, ur.String())
	case errors.Is(err, sources.NoSuchEntryError("")):
		models.SendErrorMessage(ctx, http.StatusNotFound, "not found")
	case errors.Is(err, sources.InvalidArgumentError("")):
		models.SendError(ctx, http.StatusBadRequest, err)
	default:
		slog.Error("database error", "err", err)
		models.SendError(ctx, http.StatusInternalServerError, err)
	}
}

func validateHeaders(headers []string) error {
	for _, header := range headers {
		if k, _, ok := strings.Cut(header, ":"); !ok || strings.TrimSpace(k) == "" {
			return sources.InvalidArgumentError(
				fmt.Sprintf("header %q is invalid", header))
		}
	}
	return nil
}

type feedResult struct {
	Feeds []*feed `json:"feeds"`
}

// viewFeeds is an endpoint that returns all feeds.
//
//	@Summary		Returns feeds.
//	@Description	Returns all feed configurations and metadata.
//	@Param			id		path	int		true	"Feed ID"
//	@Param			stats	query	bool	false	"Enable statistic"
//	@Produce		json
//	@Success		200	{object}	feedResult
//	@Failure		400	{object}	models.Error	"could not parse stats"
//	@Failure		404	{object}	models.Error
//	@Failure		500	{object}	models.Error
//	@Router			/sources/{id}/feeds [get]
func (c *Controller) viewFeeds(ctx *gin.Context) {
	var input struct {
		SourceID int64 `uri:"id"`
	}
	if err := ctx.ShouldBindUri(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	stats, ok := showStats(ctx)
	if !ok {
		models.SendErrorMessage(ctx, http.StatusBadRequest, "could not parse stats")
		return
	}
	feeds := []*feed{}
	switch err := c.sm.Feeds(input.SourceID, func(fi *sources.FeedInfo) {
		feeds = append(feeds, newFeed(fi))
	}, stats); {
	case err == nil:
		ctx.JSON(http.StatusOK, feedResult{Feeds: feeds})
	case errors.Is(err, sources.NoSuchEntryError("")):
		models.SendError(ctx, http.StatusNotFound, err)
	default:
		slog.Error("database error", "err", err)
		models.SendError(ctx, http.StatusInternalServerError, err)
	}
}

// createFeed is an endpoint that creates a feed.
//
//	@Summary		Creates a feed.
//	@Description	Creates a feed with the specified configuration.
//	@Param			url	formData	web.createFeed.inputForm	true	"feed configuration"
//	@Accept			multipart/form-data
//	@Produce		json
//	@Success		201	{array}		models.ID
//	@Failure		400	{object}	models.Error	"could not parse stats"
//	@Failure		404	{object}	models.Error
//	@Failure		500	{object}	models.Error
//	@Router			/sources/{id}/feeds [post]
func (c *Controller) createFeed(ctx *gin.Context) {
	type inputForm struct {
		SourceID int64  `uri:"id"`
		Label    string `form:"label" binding:"required,min=1"`
		URL      string `form:"url" binding:"required,url"`
		LogLevel string `form:"log_level" binding:"oneof=debug info warn error ''"`
	}
	input := inputForm{}
	if err := errors.Join(ctx.ShouldBind(&input), ctx.ShouldBindUri(&input)); err != nil {
		models.SendError(ctx, http.StatusBadRequest, err)
		return
	}
	var logLevel config.FeedLogLevel
	if input.LogLevel == "" {
		logLevel = c.cfg.Sources.FeedLogLevel
	} else {
		logLevel, _ = config.ParseFeedLogLevel(input.LogLevel)
	}
	parsed, _ := url.Parse(input.URL)
	switch feedID, err := c.sm.AddFeed(
		input.SourceID,
		input.Label,
		parsed,
		logLevel,
	); {
	case err == nil:
		ctx.JSON(http.StatusCreated, models.ID{ID: feedID})
	case errors.Is(err, sources.NoSuchEntryError("")):
		models.SendError(ctx, http.StatusNotFound, err)
	case errors.Is(err, sources.InvalidArgumentError("")):
		models.SendError(ctx, http.StatusBadRequest, err)
	default:
		slog.Error("database error", "err", err)
		models.SendError(ctx, http.StatusInternalServerError, err)
	}
}

// updateFeed is an endpoint that updates a feed.
//
//	@Summary		Updates a feed.
//	@Description	Updates a feed with the specified configuration.
//	@Param			id	path	int	true	"Feed ID"
//	@Produce		json
//	@Success		200	{object}	models.Success
//	@Failure		400	{object}	models.Error
//	@Failure		404	{object}	models.Error
//	@Failure		500	{object}	models.Error
//	@Router			/sources/feeds/{id} [put]
func (c *Controller) updateFeed(ctx *gin.Context) {
	var input struct {
		FeedID int64 `uri:"id"`
	}
	if err := ctx.ShouldBindUri(&input); err != nil {
		models.SendError(ctx, http.StatusBadRequest, err)
		return
	}
	switch updated, err := c.sm.UpdateFeed(input.FeedID, func(fu *sources.FeedUpdater) error {
		// label
		if label, ok := ctx.GetPostForm("label"); ok {
			if err := fu.UpdateLabel(label); err != nil {
				return err
			}
		}
		// log_level
		if lvl, ok := ctx.GetPostForm("log_level"); ok {
			level, err := config.ParseFeedLogLevel(lvl)
			if err != nil {
				return sources.InvalidArgumentError(
					fmt.Sprintf("'log_level is invalid: %v", err))
			}
			if err := fu.UpdateLogLevel(level); err != nil {
				return err
			}
		}
		return nil
	}); {
	case err == nil:
		var msg string
		if updated {
			msg = "updated"
		} else {
			msg = "not updated"
		}
		models.SendSuccess(ctx, http.StatusOK, msg)
	case errors.Is(err, sources.NoSuchEntryError("")):
		models.SendError(ctx, http.StatusNotFound, err)
	case errors.Is(err, sources.InvalidArgumentError("")):
		models.SendError(ctx, http.StatusBadRequest, err)
	default:
		slog.Error("database error", "err", err)
		models.SendError(ctx, http.StatusInternalServerError, err)
	}
}

// viewFeed is an endpoint that returns all feeds.
//
//	@Summary		Returns feeds.
//	@Description	Returns all feed configurations and metadata.
//	@Param			id		path	int		true	"Feed ID"
//	@Param			stats	query	bool	false	"Enable statistic"
//	@Produce		json
//	@Success		200	{object}	feed
//	@Failure		400	{object}	models.Error
//	@Failure		404	{object}	models.Error	"feed not found"
//	@Router			/sources/feeds/{id} [get]
func (c *Controller) viewFeed(ctx *gin.Context) {
	var input struct {
		FeedID int64 `uri:"id"`
	}
	if err := ctx.ShouldBindUri(&input); err != nil {
		models.SendError(ctx, http.StatusBadRequest, err)
		return
	}
	stats, ok := showStats(ctx)
	if !ok {
		return
	}
	fi := c.sm.Feed(input.FeedID, stats)
	if fi == nil {
		models.SendErrorMessage(ctx, http.StatusNotFound, "feed not found")
		return
	}
	ctx.JSON(http.StatusOK, newFeed(fi))
}

// deleteFeed is an endpoint that deletes the feed with specified id.
//
//	@Summary		Deletes a feed.
//	@Description	Deletes the feed configuration with the specified id.
//	@Param			id	path	int	true	"Feed ID"
//	@Produce		json
//	@Success		200	{object}	models.Success	"deleted"
//	@Failure		400	{object}	models.Error
//	@Failure		404	{object}	models.Error
//	@Failure		500	{object}	models.Error
//	@Router			/sources/feeds/{id} [delete]
func (c *Controller) deleteFeed(ctx *gin.Context) {
	var input struct {
		FeedID int64 `uri:"id"`
	}
	if err := ctx.ShouldBindUri(&input); err != nil {
		models.SendError(ctx, http.StatusBadRequest, err)
		return
	}
	switch err := c.sm.RemoveFeed(input.FeedID); {
	case err == nil:
		models.SendSuccess(ctx, http.StatusOK, "deleted")
	case errors.Is(err, sources.NoSuchEntryError("")):
		models.SendError(ctx, http.StatusNotFound, err)
	default:
		slog.Error("removing feed failed", "err", err)
		models.SendError(ctx, http.StatusInternalServerError, err)
	}
}

// feedLog is an endpoint that returns all logs for a feed.
//
//	@Summary		Updates a feed.
//	@Description	Updates a feed with the specified configuration.
//	@Param			id	path	int	true	"Feed ID"
//	@Produce		json
//	@Success		200	{object}	web.feedLogs.feedLogEntries
//	@Failure		400	{object}	models.Error	"could not parse id"
//	@Failure		500	{object}	models.Error
//	@Router			/sources/feeds/{id}/log [get]
func (c *Controller) feedLog(ctx *gin.Context) {
	feedID, ok := parse(ctx, toInt64, ctx.Param("id"))
	if !ok {
		models.SendErrorMessage(ctx, http.StatusBadRequest, "could not parse id")
		return
	}
	c.feedLogs(ctx, &feedID)
}

// allFeedLog is an endpoint that returns all logs for all feeds.
//
//	@Summary		Updates a feed.
//	@Description	Updates a feed with the specified configuration.
//	@Produce		json
//	@Success		200	{object}	web.feedLogs.feedLogEntries
//	@Failure		500	{object}	models.Error
//	@Router			/sources/feeds/log [get]
func (c *Controller) allFeedsLog(ctx *gin.Context) {
	c.feedLogs(ctx, nil)
}

func (c *Controller) feedLogs(ctx *gin.Context, feedID *int64) {
	type entry struct {
		FeedID  int64               `json:"feed_id"`
		Time    time.Time           `json:"time"`
		Level   config.FeedLogLevel `json:"level"`
		Message string              `json:"msg"`
	}
	type feedLogEntries struct {
		Entries []entry `json:"entries"`
		Count   *int64  `json:"count,omitempty"`
	}
	var (
		from, to      *time.Time
		search              = ctx.Query("search")
		limit, offset int64 = -1, -1
		logLevels     []config.FeedLogLevel
		count, ok     bool
	)

	if ofs := ctx.Query("offset"); ofs != "" {
		if offset, ok = parse(ctx, toInt64, ofs); !ok {
			return
		}
	}

	if lim := ctx.Query("limit"); lim != "" {
		if limit, ok = parse(ctx, toInt64, lim); !ok {
			return
		}
	}

	if cnt := ctx.Query("count"); cnt != "" {
		if count, ok = parse(ctx, strconv.ParseBool, cnt); !ok {
			return
		}
	}

	if lvls := ctx.Query("levels"); lvls != "" {
		for _, lvl := range strings.Fields(lvls) {
			logLevel, ok := parse(ctx, config.ParseFeedLogLevel, lvl)
			if !ok {
				return
			}
			logLevels = append(logLevels, logLevel)
		}
	}

	if f := ctx.Query("from"); f != "" {
		fp, ok := parse(ctx, parseTime, f)
		if !ok {
			return
		}
		from = &fp
	}

	if t := ctx.Query("to"); t != "" {
		tp, ok := parse(ctx, parseTime, t)
		if !ok {
			return
		}
		to = &tp
	}

	entries := []entry{}
	counter, err := c.sm.FeedLog(
		feedID,
		from, to,
		search,
		limit, offset, logLevels, count,
		func(
			id int64,
			t time.Time,
			lvl config.FeedLogLevel,
			msg string,
		) {
			entries = append(entries, entry{
				FeedID:  id,
				Time:    t.UTC(),
				Level:   lvl,
				Message: msg,
			})
		},
	)
	if err != nil {
		slog.Error("database error", "err", err)
		models.SendError(ctx, http.StatusInternalServerError, err)
		return
	}
	h := feedLogEntries{Entries: entries}
	if count {
		h.Count = &counter
	}
	ctx.JSON(http.StatusOK, h)
}

// defaultMessage returns the default message.
//
//	@Summary		Returns the default message.
//	@Description	Returns the message that is displayed on visiting the sources page.
//	@Produce		json
//	@Success		200	{object}	models.Success
//	@Router			/sources/message [get]
func (c *Controller) defaultMessage(ctx *gin.Context) {
	models.SendSuccess(ctx, http.StatusOK, c.cfg.Sources.DefaultMessage)
}

// keepFeedTime returns how long feeds logs are kept before being deleted
//
//	@Summary		Returns how long feed logs are kept.
//	@Description	Returns the time it takes until old feed entries are deleted.
//	@Produce		json
//	@Success		200	{object}	web.keepFeedTime.keepFeedTimeConfig
//	@Router			/sources/feeds/keep [get]
func (c *Controller) keepFeedTime(ctx *gin.Context) {
	type keepFeedTimeConfig struct {
		KeepFeedTime time.Duration `json:"keep_feed_time"`
	}
	ctx.JSON(http.StatusOK, keepFeedTimeConfig{KeepFeedTime: c.cfg.Sources.KeepFeedLogs})
}

// attentionSources returns a list of sources that need attention.
//
//	@Summary		Returns a list of sources that need attention.
//	@Description	All sources that had a change and should be reviewed are returned.
//	@Param			all	query	int	false	"Return all sources"
//	@Produce		json
//	@Success		200	{array}		web.attentionSources.attention
//	@Failure		400	{object}	models.Error	"could not parse all"
//	@Router			/sources/attention [get]
func (c *Controller) attentionSources(ctx *gin.Context) {
	all, ok := parse(ctx, strconv.ParseBool, ctx.DefaultQuery("all", "false"))
	if !ok {
		models.SendErrorMessage(ctx, http.StatusBadRequest, "could not parse all")
		return
	}
	type attention struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	list := []attention{}
	c.sm.AttentionSources(all, func(id int64, name string) {
		list = append(list, attention{ID: id, Name: name})
	})
	ctx.JSON(http.StatusOK, list)
}

// defaultSourceConfig returns the default source configuration.
//
//	@Summary		Returns the default configuration.
//	@Description	Returns the default parameters for the source configuration.
//	@Produce		json
//	@Success		200	{object}	web.defaultSourceConfig.sourceConfig
//	@Router			/sources/default [get]
func (c *Controller) defaultSourceConfig(ctx *gin.Context) {
	type sourceConfig struct {
		Slots          int                 `json:"slots"`
		Rate           float64             `json:"rate"`
		LogLevel       config.FeedLogLevel `json:"log_level"`
		StrictMode     bool                `json:"strict_mode"`
		Secure         bool                `json:"secure"`
		SignatureCheck bool                `json:"signature_check"`
		Age            sourceAge           `json:"age"`
	}
	cfg := c.cfg.Sources
	ctx.JSON(http.StatusOK, sourceConfig{
		Slots:          cfg.MaxSlotsPerSource,
		Rate:           cfg.MaxRatePerSource,
		LogLevel:       cfg.FeedLogLevel,
		StrictMode:     cfg.StrictMode,
		Secure:         cfg.Secure,
		SignatureCheck: cfg.SignatureCheck,
		Age:            sourceAge{cfg.DefaultAge},
	})
}

// pmd is an endpoint the provider metadata for a URL.
//
//	@Summary		Return the pmd.
//	@Description	Fetches and returns the provider metadata for the specified URL.
//	@Param			url	formData	web.pmd.inputForm	true	"PMD URL"
//	@Accept			multipart/form-data
//	@Produce		json
//	@Success		200	{object}	any
//	@Failure		400	{object}	models.Error		"could not parse url"
//	@Failure		502	{object}	web.pmd.messages	"could not fetch pmd"
//	@Router			/pmd [get]
func (c *Controller) pmd(ctx *gin.Context) {
	type inputForm struct {
		URL string `form:"url" binding:"required,min=1"`
	}
	input := inputForm{}
	if err := ctx.ShouldBindQuery(&input); err != nil {
		models.SendError(ctx, http.StatusBadRequest, err)
		return
	}
	type messages struct {
		Messages []string `json:"messages"`
	}
	cpmd := c.sm.PMD(input.URL)
	if !cpmd.Valid() {
		h := messages{}
		msgs := cpmd.Loaded.Messages
		if n := len(msgs); n > 0 {
			txts := make([]string, 0, n)
			for i := range msgs {
				txts = append(txts, msgs[i].Message)
			}
			h.Messages = txts
		}
		ctx.JSON(http.StatusBadGateway, h)
		return
	}
	ctx.JSON(http.StatusOK, cpmd.Loaded.Document)
}
