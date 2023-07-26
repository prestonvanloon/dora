package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"github.com/pk910/light-beaconchain-explorer/services"
	"github.com/pk910/light-beaconchain-explorer/templates"
	"github.com/pk910/light-beaconchain-explorer/types/models"
	"github.com/pk910/light-beaconchain-explorer/utils"
)

// Epoch will return the main "epoch" page using a go template
func Epoch(w http.ResponseWriter, r *http.Request) {
	var epochTemplateFiles = append(layoutTemplateFiles,
		"epoch/epoch.html",
	)
	var notfoundTemplateFiles = append(layoutTemplateFiles,
		"epoch/notfound.html",
	)

	var pageTemplate = templates.GetTemplate(epochTemplateFiles...)
	w.Header().Set("Content-Type", "text/html")

	var pageData *models.EpochPageData
	var epoch uint64
	vars := mux.Vars(r)

	if vars["epoch"] != "" {
		epoch, _ = strconv.ParseUint(vars["epoch"], 10, 64)
	} else {
		epoch = uint64(utils.TimeToEpoch(time.Now()))
	}

	pageData = getEpochPageData(epoch)
	if pageData == nil {
		data := InitPageData(w, r, "blockchain", "/epoch", fmt.Sprintf("Epoch %v", epoch), notfoundTemplateFiles)
		if handleTemplateError(w, r, "slot.go", "Slot", "blockSlot", templates.GetTemplate(notfoundTemplateFiles...).ExecuteTemplate(w, "layout", data)) != nil {
			return // an error has occurred and was processed
		}
	}

	logrus.Printf("epoch page called")
	data := InitPageData(w, r, "blockchain", "/epoch", fmt.Sprintf("Epoch %v", epoch), epochTemplateFiles)
	data.Data = pageData

	if handleTemplateError(w, r, "epoch.go", "Epoch", "", pageTemplate.ExecuteTemplate(w, "layout", data)) != nil {
		return // an error has occurred and was processed
	}
}

func getEpochPageData(epoch uint64) *models.EpochPageData {

	now := time.Now()
	currentSlot := utils.TimeToSlot(uint64(now.Unix()))
	currentEpoch := utils.EpochOfSlot(currentSlot)
	if epoch > currentEpoch {
		return nil
	}

	finalizedHead, _ := services.GlobalBeaconService.GetFinalizedBlockHead()
	slotAssignments, syncedEpochs := services.GlobalBeaconService.GetProposerAssignments(epoch, epoch)

	dbEpochs := services.GlobalBeaconService.GetDbEpochs(epoch, 1)
	if dbEpochs[0] == nil {
		return nil
	}
	dbEpoch := dbEpochs[0]
	nextEpoch := epoch + 1
	if nextEpoch > currentEpoch {
		nextEpoch = 0
	}
	firstSlot := epoch * utils.Config.Chain.Config.SlotsPerEpoch
	lastSlot := firstSlot + utils.Config.Chain.Config.SlotsPerEpoch - 1
	pageData := &models.EpochPageData{
		Epoch:                   epoch,
		PreviousEpoch:           epoch - 1,
		NextEpoch:               nextEpoch,
		Ts:                      utils.EpochToTime(epoch),
		Synchronized:            syncedEpochs[epoch],
		Finalized:               uint64(finalizedHead.Data.Header.Message.Slot) >= lastSlot,
		AttestationCount:        dbEpoch.AttestationCount,
		DepositCount:            dbEpoch.DepositCount,
		ExitCount:               dbEpoch.ExitCount,
		WithdrawalCount:         dbEpoch.WithdrawCount,
		WithdrawalAmount:        dbEpoch.WithdrawAmount,
		ProposerSlashingCount:   dbEpoch.ProposerSlashingCount,
		AttesterSlashingCount:   dbEpoch.AttesterSlashingCount,
		EligibleEther:           dbEpoch.Eligible,
		TargetVoted:             dbEpoch.VotedTarget,
		HeadVoted:               dbEpoch.VotedHead,
		TotalVoted:              dbEpoch.VotedTotal,
		SyncParticipation:       float64(dbEpoch.SyncParticipation) * 100,
		ValidatorCount:          dbEpoch.ValidatorCount,
		AverageValidatorBalance: dbEpoch.ValidatorBalance / dbEpoch.ValidatorCount,
	}
	if dbEpoch.Eligible > 0 {
		pageData.TargetVoteParticipation = float64(dbEpoch.VotedTarget) * 100.0 / float64(dbEpoch.Eligible)
		pageData.HeadVoteParticipation = float64(dbEpoch.VotedHead) * 100.0 / float64(dbEpoch.Eligible)
		pageData.TotalVoteParticipation = float64(dbEpoch.VotedTotal) * 100.0 / float64(dbEpoch.Eligible)
	}

	// load slots
	pageData.Slots = make([]*models.EpochPageDataSlot, 0)
	dbSlots := services.GlobalBeaconService.GetDbBlocksForSlots(uint64(lastSlot), uint32(utils.Config.Chain.Config.SlotsPerEpoch), true)
	dbIdx := 0
	dbCnt := len(dbSlots)
	blockCount := uint64(0)
	for slotIdx := int64(lastSlot); slotIdx >= int64(firstSlot); slotIdx-- {
		slot := uint64(slotIdx)
		haveBlock := false
		for dbIdx < dbCnt && dbSlots[dbIdx] != nil && dbSlots[dbIdx].Slot == slot {
			dbSlot := dbSlots[dbIdx]
			dbIdx++
			blockStatus := uint8(1)
			if dbSlot.Orphaned {
				blockStatus = 2
				pageData.OrphanedCount++
			} else {
				pageData.CanonicalCount++
			}

			slotData := &models.EpochPageDataSlot{
				Slot:                  slot,
				Epoch:                 utils.EpochOfSlot(slot),
				Ts:                    utils.SlotToTime(slot),
				Status:                blockStatus,
				Proposer:              dbSlot.Proposer,
				ProposerName:          "", // TODO
				AttestationCount:      dbSlot.AttestationCount,
				DepositCount:          dbSlot.DepositCount,
				ExitCount:             dbSlot.ExitCount,
				ProposerSlashingCount: dbSlot.ProposerSlashingCount,
				AttesterSlashingCount: dbSlot.AttesterSlashingCount,
				SyncParticipation:     float64(dbSlot.SyncParticipation) * 100,
				EthTransactionCount:   dbSlot.EthTransactionCount,
				EthBlockNumber:        dbSlot.EthBlockNumber,
				Graffiti:              dbSlot.Graffiti,
				BlockRoot:             dbSlot.Root,
			}
			pageData.Slots = append(pageData.Slots, slotData)
			blockCount++
			haveBlock = true
		}

		if !haveBlock {
			slotData := &models.EpochPageDataSlot{
				Slot:         slot,
				Epoch:        epoch,
				Ts:           utils.SlotToTime(slot),
				Scheduled:    slot >= currentSlot,
				Status:       0,
				Proposer:     slotAssignments[slot],
				ProposerName: "", // TODO
			}
			if slotData.Scheduled {
				pageData.ScheduledCount++
			} else {
				pageData.MissedCount++
			}
			pageData.Slots = append(pageData.Slots, slotData)
			blockCount++
		}
	}
	pageData.BlockCount = uint64(blockCount)

	return pageData
}