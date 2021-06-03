package orka

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

type stepCreateImage struct {
	failedCommit bool
	failedSave   bool
}

func (s *stepCreateImage) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	vmid := state.Get("vmid").(string)
	token := state.Get("token").(string)

	if config.NoCreateImage {
		ui.Say("Skipping image creation because of 'no_create_image' being set")
		return multistep.ActionContinue
	}

	ui.Say(fmt.Sprintf("Image creation is using VM ID [%s]", vmid))
	ui.Say(fmt.Sprintf("Image name is [%s]", config.ImageName))

	// HTTP Client.

	client := &http.Client{
		Timeout: time.Minute * 30,
	}

	if config.ImagePrecopy {
		// If we are using the pre-copy logic, then we just re-commit the image back.

		ui.Say("Committing existing image since pre-copy is being used")
		ui.Say("Please wait as this can take a little while...")

		imageCommitRequestData := ImageCommitRequest{vmid}
		imageCommitRequestDataJSON, _ := json.Marshal(imageCommitRequestData)
		imageCommitRequest, err := http.NewRequest(
			http.MethodPost,
			fmt.Sprintf("%s/%s", config.OrkaEndpoint, "resources/image/commit"),
			bytes.NewBuffer(imageCommitRequestDataJSON),
		)
		imageCommitRequest.Header.Set("Content-Type", "application/json")
		imageCommitRequest.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		imageCommitResponse, err := client.Do(imageCommitRequest)

		if err != nil {
			s.failedCommit = true
			e := fmt.Errorf("%s [%s]", OrkaAPIRequestErrorMessage, err)
			ui.Error(e.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}

		var imageCommitResponseData ImageCommitResponse
		imageCommitResponseBytes, _ := ioutil.ReadAll(imageCommitResponse.Body)
		json.Unmarshal(imageCommitResponseBytes, &imageCommitResponseData)
		imageCommitResponse.Body.Close()

		if imageCommitResponse.StatusCode != 200 {
			s.failedCommit = true
			e := fmt.Errorf("Error committing image [%s]", imageCommitResponse.Status)
			ui.Error(e.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}

		ui.Say(fmt.Sprintf("Image comitted [%s] [%s]", imageCommitResponse.Status, imageCommitResponseData.Message))
	} else {
		// By default we use the save endpoint to generate a new base image from
		// the running VM's current image.

		ui.Say(fmt.Sprintf("Saving new image [%s]", config.ImageName))
		ui.Say("Please wait as this can take a little while...")

		imageSaveRequestData := ImageSaveRequest{vmid, config.ImageName}
		imageSaveRequestDataJSON, _ := json.Marshal(imageSaveRequestData)
		imageSaveRequest, err := http.NewRequest(
			http.MethodPost,
			fmt.Sprintf("%s/%s", config.OrkaEndpoint, "resources/image/save"),
			bytes.NewBuffer(imageSaveRequestDataJSON),
		)
		imageSaveRequest.Header.Set("Content-Type", "application/json")
		imageSaveRequest.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		imageSaveResponse, err := client.Do(imageSaveRequest)

		if err != nil {
			s.failedSave = true
			e := fmt.Errorf("%s [%s]", OrkaAPIRequestErrorMessage, err)
			ui.Error(e.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}

		var imageSaveResponseData ImageSaveResponse
		imageSaveResponseBytes, _ := ioutil.ReadAll(imageSaveResponse.Body)
		json.Unmarshal(imageSaveResponseBytes, &imageSaveResponseData)
		imageSaveResponse.Body.Close()

		if imageSaveResponse.StatusCode != 200 {
			s.failedSave = true
			e := fmt.Errorf("%s [%s]", OrkaAPIResponseErrorMessage, imageSaveResponse.Status)
			ui.Error(e.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}

		ui.Say(fmt.Sprintf("Image saved [%s] [%s]", imageSaveResponse.Status, imageSaveResponseData.Message))
	}

	return multistep.ActionContinue
}

func (s *stepCreateImage) Cleanup(state multistep.StateBag) {
	ui := state.Get("ui").(packer.Ui)
	vmid := state.Get("vmid").(string)
	_, cancelled := state.GetOk(multistep.StateCancelled)
	_, halted := state.GetOk(multistep.StateHalted)

	if s.failedCommit || s.failedSave {
		// TODO: Automatically clean up? Make a user-flag?
		ui.Say("Commit or save failed - please check Orka to see if any artifacts were left behind")
		return
	}

	if !cancelled && !halted {
		return
	}

	if vmid == "" {
		return
	}
}
