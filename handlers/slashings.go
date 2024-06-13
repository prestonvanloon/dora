package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/dbtypes"
	"github.com/ethpandaops/dora/services"
	"github.com/ethpandaops/dora/templates"
	"github.com/ethpandaops/dora/types/models"
	"github.com/ethpandaops/dora/utils"
	"github.com/sirupsen/logrus"
)

// Slashings will return the filtered "slashings" page using a go template
func Slashings(w http.ResponseWriter, r *http.Request) {
	var templateFiles = append(layoutTemplateFiles,
		"slashings/slashings.html",
		"_svg/professor.html",
	)

	var pageTemplate = templates.GetTemplate(templateFiles...)
	data := InitPageData(w, r, "validators", "/validators/slashings", "Slashings", templateFiles)

	urlArgs := r.URL.Query()
	var pageSize uint64 = 50
	if urlArgs.Has("c") {
		pageSize, _ = strconv.ParseUint(urlArgs.Get("c"), 10, 64)
	}
	var pageIdx uint64 = 1
	if urlArgs.Has("p") {
		pageIdx, _ = strconv.ParseUint(urlArgs.Get("p"), 10, 64)
		if pageIdx < 1 {
			pageIdx = 1
		}
	}

	var minSlot uint64
	var maxSlot uint64
	var minIndex uint64
	var maxIndex uint64
	var vname string
	var sname string
	var withReason uint64
	var withOrphaned uint64

	if urlArgs.Has("f") {
		if urlArgs.Has("f.mins") {
			minSlot, _ = strconv.ParseUint(urlArgs.Get("f.mins"), 10, 64)
		}
		if urlArgs.Has("f.maxs") {
			maxSlot, _ = strconv.ParseUint(urlArgs.Get("f.maxs"), 10, 64)
		}
		if urlArgs.Has("f.mini") {
			minIndex, _ = strconv.ParseUint(urlArgs.Get("f.mini"), 10, 64)
		}
		if urlArgs.Has("f.maxi") {
			maxIndex, _ = strconv.ParseUint(urlArgs.Get("f.maxi"), 10, 64)
		}
		if urlArgs.Has("f.vname") {
			vname = urlArgs.Get("f.vname")
		}
		if urlArgs.Has("f.sname") {
			sname = urlArgs.Get("f.sname")
		}
		if urlArgs.Has("f.reason") {
			withReason, _ = strconv.ParseUint(urlArgs.Get("f.reason"), 10, 64)
		}
		if urlArgs.Has("f.orphaned") {
			withOrphaned, _ = strconv.ParseUint(urlArgs.Get("f.orphaned"), 10, 64)
		}
	} else {
		withOrphaned = 1
	}
	var pageError error
	pageError = services.GlobalCallRateLimiter.CheckCallLimit(r, 2)
	if pageError == nil {
		data.Data, pageError = getFilteredSlashingsPageData(pageIdx, pageSize, minSlot, maxSlot, minIndex, maxIndex, vname, sname, uint8(withReason), uint8(withOrphaned))
	}
	if pageError != nil {
		handlePageError(w, r, pageError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	if handleTemplateError(w, r, "slashings.go", "Slashings", "", pageTemplate.ExecuteTemplate(w, "layout", data)) != nil {
		return // an error has occurred and was processed
	}
}

func getFilteredSlashingsPageData(pageIdx uint64, pageSize uint64, minSlot uint64, maxSlot uint64, minIndex uint64, maxIndex uint64, vname string, sname string, withReason uint8, withOrphaned uint8) (*models.SlashingsPageData, error) {
	pageData := &models.SlashingsPageData{}
	pageCacheKey := fmt.Sprintf("slashings:%v:%v:%v:%v:%v:%v:%v:%v:%v:%v", pageIdx, pageSize, minSlot, maxSlot, minIndex, maxIndex, vname, sname, withReason, withOrphaned)
	pageRes, pageErr := services.GlobalFrontendCache.ProcessCachedPage(pageCacheKey, true, pageData, func(_ *services.FrontendCacheProcessingPage) interface{} {
		return buildFilteredSlashingsPageData(pageIdx, pageSize, minSlot, maxSlot, minIndex, maxIndex, vname, sname, withReason, withOrphaned)
	})
	if pageErr == nil && pageRes != nil {
		resData, resOk := pageRes.(*models.SlashingsPageData)
		if !resOk {
			return nil, ErrInvalidPageModel
		}
		pageData = resData
	}
	return pageData, pageErr
}

func buildFilteredSlashingsPageData(pageIdx uint64, pageSize uint64, minSlot uint64, maxSlot uint64, minIndex uint64, maxIndex uint64, vname string, sname string, withReason uint8, withOrphaned uint8) *models.SlashingsPageData {
	filterArgs := url.Values{}
	if minSlot != 0 {
		filterArgs.Add("f.mins", fmt.Sprintf("%v", minSlot))
	}
	if maxSlot != 0 {
		filterArgs.Add("f.maxs", fmt.Sprintf("%v", maxSlot))
	}
	if minIndex != 0 {
		filterArgs.Add("f.mini", fmt.Sprintf("%v", minIndex))
	}
	if maxIndex != 0 {
		filterArgs.Add("f.maxi", fmt.Sprintf("%v", maxIndex))
	}
	if vname != "" {
		filterArgs.Add("f.vname", vname)
	}
	if sname != "" {
		filterArgs.Add("f.sname", sname)
	}
	if withReason != 0 {
		filterArgs.Add("f.reason", fmt.Sprintf("%v", withReason))
	}
	if withOrphaned != 0 {
		filterArgs.Add("f.orphaned", fmt.Sprintf("%v", withOrphaned))
	}

	pageData := &models.SlashingsPageData{
		FilterMinSlot:       minSlot,
		FilterMaxSlot:       maxSlot,
		FilterMinIndex:      minIndex,
		FilterMaxIndex:      maxIndex,
		FilterValidatorName: vname,
		FilterSlasherName:   sname,
		FilterWithReason:    withReason,
		FilterWithOrphaned:  withOrphaned,
	}
	logrus.Debugf("slashings page called: %v:%v [%v,%v,%v,%v,%v,%v]", pageIdx, pageSize, minSlot, maxSlot, minIndex, maxIndex, vname, sname)
	if pageIdx == 1 {
		pageData.IsDefaultPage = true
	}

	if pageSize > 100 {
		pageSize = 100
	}
	pageData.PageSize = pageSize
	pageData.TotalPages = pageIdx
	pageData.CurrentPageIndex = pageIdx
	if pageIdx > 1 {
		pageData.PrevPageIndex = pageIdx - 1
	}

	// load slashings
	finalizedEpoch, _ := services.GlobalBeaconService.GetFinalizedEpoch()
	if finalizedEpoch < 0 {
		finalizedEpoch = 0
	}

	slashingFilter := &dbtypes.SlashingFilter{
		MinSlot:       minSlot,
		MaxSlot:       maxSlot,
		MinIndex:      minIndex,
		MaxIndex:      maxIndex,
		ValidatorName: vname,
		SlasherName:   sname,
		WithReason:    dbtypes.SlashingReason(withReason),
		WithOrphaned:  withOrphaned,
	}

	offset := (pageIdx - 1) * pageSize

	dbSlashings, totalRows, err := db.GetSlashingsFiltered(offset, uint32(pageSize), uint64(finalizedEpoch), slashingFilter)
	if err != nil {
		panic(err)
	}

	validatorSetRsp := services.GlobalBeaconService.GetCachedValidatorSet()
	validatorActivityMap, validatorActivityMax := services.GlobalBeaconService.GetValidatorActivity()

	for _, slashing := range dbSlashings {
		slashingData := &models.SlashingsPageDataSlashing{
			SlotNumber:      slashing.SlotNumber,
			SlotRoot:        slashing.SlotRoot,
			Time:            utils.SlotToTime(slashing.SlotNumber),
			Orphaned:        slashing.Orphaned,
			Reason:          uint8(slashing.Reason),
			ValidatorIndex:  slashing.ValidatorIndex,
			ValidatorName:   services.GlobalBeaconService.GetValidatorName(slashing.ValidatorIndex),
			SlasherIndex:    slashing.SlasherIndex,
			SlasherName:     services.GlobalBeaconService.GetValidatorName(slashing.SlasherIndex),
			ValidatorStatus: "",
		}

		validator := validatorSetRsp[phase0.ValidatorIndex(slashing.ValidatorIndex)]
		if validator == nil {
			slashingData.ValidatorStatus = "Unknown"
		} else {
			slashingData.Balance = uint64(validator.Balance)

			if strings.HasPrefix(validator.Status.String(), "pending") {
				slashingData.ValidatorStatus = "Pending"
			} else if validator.Status == v1.ValidatorStateActiveOngoing {
				slashingData.ValidatorStatus = "Active"
				slashingData.ShowUpcheck = true
			} else if validator.Status == v1.ValidatorStateActiveExiting {
				slashingData.ValidatorStatus = "Exiting"
				slashingData.ShowUpcheck = true
			} else if validator.Status == v1.ValidatorStateActiveSlashed {
				slashingData.ValidatorStatus = "Slashed"
				slashingData.ShowUpcheck = true
			} else if validator.Status == v1.ValidatorStateExitedUnslashed {
				slashingData.ValidatorStatus = "Exited"
			} else if validator.Status == v1.ValidatorStateExitedSlashed {
				slashingData.ValidatorStatus = "Slashed"
			} else {
				slashingData.ValidatorStatus = validator.Status.String()
			}

			if slashingData.ShowUpcheck {
				slashingData.UpcheckActivity = validatorActivityMap[uint64(validator.Index)]
				slashingData.UpcheckMaximum = uint8(validatorActivityMax)
			}
		}

		pageData.Slashings = append(pageData.Slashings, slashingData)
	}
	pageData.SlashingCount = uint64(len(pageData.Slashings))

	if pageData.SlashingCount > 0 {
		pageData.FirstIndex = pageData.Slashings[0].SlotNumber
		pageData.LastIndex = pageData.Slashings[pageData.SlashingCount-1].SlotNumber
	}

	pageData.TotalPages = totalRows / pageSize
	if totalRows%pageSize > 0 {
		pageData.TotalPages++
	}
	pageData.LastPageIndex = pageData.TotalPages
	if pageIdx < pageData.TotalPages {
		pageData.NextPageIndex = pageIdx + 1
	}

	pageData.FirstPageLink = fmt.Sprintf("/validators/slashings?f&%v&c=%v", filterArgs.Encode(), pageData.PageSize)
	pageData.PrevPageLink = fmt.Sprintf("/validators/slashings?f&%v&c=%v&p=%v", filterArgs.Encode(), pageData.PageSize, pageData.PrevPageIndex)
	pageData.NextPageLink = fmt.Sprintf("/validators/slashings?f&%v&c=%v&p=%v", filterArgs.Encode(), pageData.PageSize, pageData.NextPageIndex)
	pageData.LastPageLink = fmt.Sprintf("/validators/slashings?f&%v&c=%v&p=%v", filterArgs.Encode(), pageData.PageSize, pageData.LastPageIndex)

	return pageData
}
