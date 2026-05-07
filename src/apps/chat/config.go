package chat

type Config struct {
	LobbyMaxCapacity int
	WorldCooldown    int
	MsgMaxLength     int
	WorldMsgKeep     int
	PrivateKeepDays  int
	SystemMsgKeep    int
}

func GetConfig() *Config {
	return &Config{
		LobbyMaxCapacity: 100,
		WorldCooldown:    5,
		MsgMaxLength:     200,
		WorldMsgKeep:     100,
		PrivateKeepDays:  30,
		SystemMsgKeep:    50,
	}
}
