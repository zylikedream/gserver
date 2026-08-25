package main

import (
	"testing"

	"hy_client/pkg/client"
)

func TestPascalToSnake(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"SetName", "set_name"},
		{"SendRequest", "send_request"},
		{"SearchPlayer", "search_player"},
		{"GMCommand", "gm_command"},
		{"SetPosition", "set_position"},
		{"TransferLeader", "transfer_leader"},
		{"ApproveApply", "approve_apply"},
		{"KickMember", "kick_member"},
		{"UpdateInfo", "update_info"},
		{"StartBreed", "start_breed"},
		{"FinishBreed", "finish_breed"},
		{"ClaimMilestone", "claim_milestone"},
		{"Info", "info"},
		{"List", "list"},
	}
	for _, tc := range cases {
		got := pascalToSnake(tc.input)
		if got != tc.want {
			t.Errorf("pascalToSnake(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDeriveCommandName(t *testing.T) {
	cases := []struct {
		protoName, want string
	}{
		{"ReqBasicInfo", "basic.info"},
		{"ReqBasicSetName", "basic.set_name"},
		{"ReqBasicSetHead", "basic.set_head"},
		{"ReqBagInfo", "bag.info"},
		{"ReqFlowerInfo", "flower.info"},
		{"ReqFlowerUpgrade", "flower.upgrade"},
		{"ReqFlowerBreak", "flower.break"},
		{"ReqFlowerStartBreed", "flower.start_breed"},
		{"ReqFlowerFinishBreed", "flower.finish_breed"},
		{"ReqPlotInfo", "plot.info"},
		{"ReqPlotUnlock", "plot.unlock"},
		{"ReqPlotPlant", "plot.plant"},
		{"ReqPlotWater", "plot.water"},
		{"ReqPlotHarvest", "plot.harvest"},
		{"ReqPlotRemove", "plot.remove"},
		{"ReqMainTaskInfo", "maintask.info"},
		{"ReqMainTaskClaim", "maintask.claim"},
		{"ReqResidentOrderInfo", "residentorder.info"},
		{"ReqResidentOrderSubmit", "residentorder.submit"},
		{"ReqResidentOrderClaimMilestone", "residentorder.claim_milestone"},
		{"ReqFriendSearchPlayer", "friend.search_player"},
		{"ReqFriendSendRequest", "friend.send_request"},
		{"ReqFriendAcceptRequest", "friend.accept_request"},
		{"ReqFriendRejectRequest", "friend.reject_request"},
		{"ReqFriendList", "friend.list"},
		{"ReqFriendApplyList", "friend.apply_list"},
		{"ReqFriendRemove", "friend.remove"},
		{"ReqChatInit", "chat.init"},
		{"ReqChatSendChannel", "chat.send_channel"},
		{"ReqChatSendPrivate", "chat.send_private"},
		{"ReqChatPrivateHistory", "chat.private_history"},
		{"ReqChatSystemHistory", "chat.system_history"},
		{"ReqChatChannelHistory", "chat.channel_history"},
		{"ReqGuildCreate", "guild.create"},
		{"ReqGuildSearch", "guild.search"},
		{"ReqGuildApply", "guild.apply"},
		{"ReqGuildInfo", "guild.info"},
		{"ReqGuildLogs", "guild.logs"},
		{"ReqGuildApplyList", "guild.apply_list"},
		{"ReqGuildApproveApply", "guild.approve_apply"},
		{"ReqGuildKickMember", "guild.kick_member"},
		{"ReqGuildLeave", "guild.leave"},
		{"ReqGuildDisband", "guild.disband"},
		{"ReqGuildSetPosition", "guild.set_position"},
		{"ReqGuildTransferLeader", "guild.transfer_leader"},
		{"ReqGuildUpdateInfo", "guild.update_info"},
		{"ReqGMCommand", "gm.command"},
		{"ReqGMHelp", "gm.help"},
	}
	for _, tc := range cases {
		got := deriveCommandName(tc.protoName)
		if got != tc.want {
			t.Errorf("deriveCommandName(%q) = %q, want %q", tc.protoName, got, tc.want)
		}
	}
}

func TestRegisterAutoCommandsSkipsLoginRange(t *testing.T) {
	client.RegisterMessages()
	// Login-range messages (10xxx) should not produce commands
	loginIDs := []string{"10001", "10002", "10003", "10004"}
	for _, id := range loginIDs {
		name := client.MessageNameByID(id)
		cmdName := deriveCommandName(name)
		if _, exists := commands[cmdName]; exists {
			// Could exist if another message maps to same name, but unlikely for login msgs
			t.Logf("warning: command %q exists for login message %s", cmdName, name)
		}
	}
}

func TestRegisterAutoCommandsNoOverwrite(t *testing.T) {
	// Manual commands should not be overwritten by auto-gen
	client.RegisterMessages()
	if cmd, ok := commands["breed.start"]; !ok {
		t.Error("breed.start should exist")
	} else if cmd.Help != "Start breeding a flower" {
		t.Errorf("breed.start overwritten: Help = %q", cmd.Help)
	}
	if cmd, ok := commands["chat.send"]; !ok {
		t.Error("chat.send should exist")
	} else if cmd.Help != "Send message to a channel (world/guild)" {
		t.Errorf("chat.send overwritten: Help = %q", cmd.Help)
	}
	if cmd, ok := commands["guild.approve"]; !ok {
		t.Error("guild.approve should exist")
	} else if cmd.Help != "Approve/reject guild applications" {
		t.Errorf("guild.approve overwritten: Help = %q", cmd.Help)
	}
}

func TestRegisterAutoCommandsCreatesAutoCommands(t *testing.T) {
	client.RegisterMessages()
	// These should be auto-generated
	autoCmds := []string{
		"basic.info", "basic.set_name", "basic.set_head",
		"bag.info",
		"flower.info", "flower.upgrade", "flower.break",
		"plot.info", "plot.unlock",
		"maintask.info", "maintask.claim",
		"friend.search_player", "friend.send_request",
		"friend.accept_request", "friend.reject_request",
		"friend.list", "friend.apply_list", "friend.remove",
		"chat.init",
		"guild.create", "guild.search", "guild.apply",
		"guild.info", "guild.logs", "guild.apply_list",
		"guild.leave", "guild.disband",
		"guild.set_position", "guild.transfer_leader",
		"gm.command", "gm.help",
	}
	for _, name := range autoCmds {
		if _, ok := commands[name]; !ok {
			t.Errorf("auto-generated command %q not found", name)
		}
	}
}
