package callrouting

import (
	"time"

	"vozko/domain/callrouting"
	callrouting_usecase "vozko/usecases/callrouting"
)

type HoldMusicDTO struct {
	PresetID string `json:"presetId,omitempty" example:"bossa_nova"`
	MediaID  string `json:"mediaId,omitempty" example:""`
}

type QueueRequest struct {
	Name           string       `json:"name" example:"Suporte"`
	Strategy       string       `json:"strategy" enums:"longest_idle,round_robin,fewest_calls,random" example:"longest_idle"`
	DepartmentID   string       `json:"departmentId,omitempty" example:""`
	MemberUserIDs  []string     `json:"memberUserIds,omitempty" example:"7c1f0a52-5b7e-4d0c-9a1b-2c3d4e5f6a7b"`
	RingSeconds    int          `json:"ringSeconds" minimum:"5" maximum:"60" example:"15"`
	MaxWaitSeconds int          `json:"maxWaitSeconds" minimum:"10" maximum:"3600" example:"300"`
	WrapUpSeconds  int          `json:"wrapUpSeconds" minimum:"0" maximum:"300" example:"10"`
	HoldMusic      HoldMusicDTO `json:"holdMusic"`
}

type QueueResponse struct {
	ID             string       `json:"id" example:"5f0c2b1e-8d2a-4c61-9b7e-1a2b3c4d5e6f"`
	Name           string       `json:"name" example:"Suporte"`
	Strategy       string       `json:"strategy" enums:"longest_idle,round_robin,fewest_calls,random" example:"longest_idle"`
	DepartmentID   string       `json:"departmentId,omitempty" example:""`
	MemberUserIDs  []string     `json:"memberUserIds" example:"7c1f0a52-5b7e-4d0c-9a1b-2c3d4e5f6a7b"`
	RingSeconds    int          `json:"ringSeconds" example:"15"`
	MaxWaitSeconds int          `json:"maxWaitSeconds" example:"300"`
	WrapUpSeconds  int          `json:"wrapUpSeconds" example:"10"`
	HoldMusic      HoldMusicDTO `json:"holdMusic"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

type QueueTargetResponse struct {
	ID      string `json:"id" example:"5f0c2b1e-8d2a-4c61-9b7e-1a2b3c4d5e6f"`
	Name    string `json:"name" example:"Suporte"`
	Waiting int    `json:"waiting" example:"2"`
	Ready   int    `json:"ready" example:"1"`
}

type WaitingCallerResponse struct {
	CallID         string `json:"callId" example:"sip-in-7d9f2c4a"`
	RemoteNumber   string `json:"remoteNumber" example:"5584994409684"`
	WaitingSeconds int    `json:"waitingSeconds" example:"42"`
}

type QueueAgentResponse struct {
	UserID string `json:"userId" example:"7c1f0a52-5b7e-4d0c-9a1b-2c3d4e5f6a7b"`
	Name   string `json:"name,omitempty" example:"Ana Souza"`
	State  string `json:"state" enums:"free,ringing,on_call,wrap_up,offline" example:"free"`
}

type AgentCountsResponse struct {
	Free    int `json:"free" example:"2"`
	Ringing int `json:"ringing" example:"0"`
	OnCall  int `json:"onCall" example:"3"`
	WrapUp  int `json:"wrapUp" example:"1"`
	Offline int `json:"offline" example:"4"`
}

type QueueLiveResponse struct {
	ID                 string                  `json:"id" example:"5f0c2b1e-8d2a-4c61-9b7e-1a2b3c4d5e6f"`
	Name               string                  `json:"name" example:"Suporte"`
	Waiting            []WaitingCallerResponse `json:"waiting"`
	LongestWaitSeconds int                     `json:"longestWaitSeconds" example:"42"`
	Agents             []QueueAgentResponse    `json:"agents"`
	Counts             AgentCountsResponse     `json:"counts"`
}

type QueueStatsResponse struct {
	QueueID                   string  `json:"queueId" example:"5f0c2b1e-8d2a-4c61-9b7e-1a2b3c4d5e6f"`
	Offered                   int     `json:"offered" example:"120"`
	Answered                  int     `json:"answered" example:"104"`
	Abandoned                 int     `json:"abandoned" example:"11"`
	TimedOut                  int     `json:"timedOut" example:"5"`
	AverageAnswerSeconds      int     `json:"averageAnswerSeconds" example:"18"`
	ServiceLevel              float64 `json:"serviceLevel" example:"0.82"`
	ServiceLevelTargetSeconds int     `json:"serviceLevelTargetSeconds" example:"20"`
}

type SettingsRequest struct {
	HoldMusic HoldMusicDTO `json:"holdMusic"`
}

type SettingsResponse struct {
	HoldMusic HoldMusicDTO `json:"holdMusic"`
}

type HoldPresetResponse struct {
	ID   string `json:"id" example:"bossa_nova"`
	Name string `json:"name" example:"Bossa nova"`
	Mood string `json:"mood" enums:"calm,elegant,professional,brazilian,modern,upbeat,seasonal" example:"brazilian"`
}

type StatusResponse struct {
	Status string `json:"status" example:"deleted"`
}

func (d HoldMusicDTO) toDomain() callrouting.HoldMusicRef {
	return callrouting.HoldMusicRef{PresetID: d.PresetID, MediaID: d.MediaID}
}

func toHoldMusicDTO(ref callrouting.HoldMusicRef) HoldMusicDTO {
	return HoldMusicDTO{PresetID: ref.PresetID, MediaID: ref.MediaID}
}

func (r QueueRequest) toDomain(workspaceID, id string) callrouting.Queue {
	return callrouting.Queue{
		ID:             id,
		WorkspaceID:    workspaceID,
		Name:           r.Name,
		Strategy:       callrouting.Strategy(r.Strategy),
		DepartmentID:   r.DepartmentID,
		MemberUserIDs:  r.MemberUserIDs,
		RingSeconds:    r.RingSeconds,
		MaxWaitSeconds: r.MaxWaitSeconds,
		WrapUpSeconds:  r.WrapUpSeconds,
		HoldMusic:      r.HoldMusic.toDomain(),
	}
}

func toQueueDTO(queue *callrouting.Queue) QueueResponse {
	members := queue.MemberUserIDs
	if members == nil {
		members = []string{}
	}
	return QueueResponse{
		ID:             queue.ID,
		Name:           queue.Name,
		Strategy:       string(queue.Strategy),
		DepartmentID:   queue.DepartmentID,
		MemberUserIDs:  members,
		RingSeconds:    queue.RingSeconds,
		MaxWaitSeconds: queue.MaxWaitSeconds,
		WrapUpSeconds:  queue.WrapUpSeconds,
		HoldMusic:      toHoldMusicDTO(queue.HoldMusic),
		CreatedAt:      queue.CreatedAt,
		UpdatedAt:      queue.UpdatedAt,
	}
}

func toQueueLiveDTO(live callrouting.QueueLive, now time.Time) QueueLiveResponse {
	waiting := make([]WaitingCallerResponse, 0, len(live.Waiting))
	for _, caller := range live.Waiting {
		waiting = append(waiting, WaitingCallerResponse{CallID: caller.CallID, RemoteNumber: caller.RemoteNumber, WaitingSeconds: seconds(now.Sub(caller.Since))})
	}
	agents := make([]QueueAgentResponse, 0, len(live.Agents))
	for _, agent := range live.Agents {
		agents = append(agents, QueueAgentResponse{UserID: agent.UserID, Name: agent.Name, State: string(agent.State)})
	}
	counts := live.AgentCounts()
	return QueueLiveResponse{
		ID:                 live.QueueID,
		Name:               live.Name,
		Waiting:            waiting,
		LongestWaitSeconds: seconds(live.LongestWait(now)),
		Agents:             agents,
		Counts: AgentCountsResponse{
			Free:    counts[callrouting.AgentFree],
			Ringing: counts[callrouting.AgentRinging],
			OnCall:  counts[callrouting.AgentOnCall],
			WrapUp:  counts[callrouting.AgentWrapUp],
			Offline: counts[callrouting.AgentOffline],
		},
	}
}

func toQueueStatsDTO(tally callrouting.QueueTally) QueueStatsResponse {
	return QueueStatsResponse{
		QueueID:                   tally.QueueID,
		Offered:                   tally.Offered,
		Answered:                  tally.Answered,
		Abandoned:                 tally.Abandoned,
		TimedOut:                  tally.TimedOut,
		AverageAnswerSeconds:      seconds(tally.AverageAnswerWait()),
		ServiceLevel:              tally.ServiceLevel(),
		ServiceLevelTargetSeconds: seconds(callrouting.ServiceLevelTarget),
	}
}

func seconds(d time.Duration) int {
	return int(d.Round(time.Second) / time.Second)
}

func toQueueTargetDTO(target callrouting_usecase.QueueTarget) QueueTargetResponse {
	return QueueTargetResponse{ID: target.ID, Name: target.Name, Waiting: target.Waiting, Ready: target.Ready}
}
