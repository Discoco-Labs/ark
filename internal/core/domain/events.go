package domain

type RoundEvent interface {
	isEvent()
}

func (r RoundStarted) isEvent()             {}
func (r RoundFinalizationStarted) isEvent() {}
func (r RoundFinalized) isEvent()           {}
func (r RoundFailed) isEvent()              {}
func (r PaymentsRegistered) isEvent()       {}
func (r PaymentsClaimed) isEvent()          {}

type RoundStarted struct {
	Id        string
	Timestamp int64
}

type RoundFinalizationStarted struct {
	Id             string
	CongestionTree []string
	Connectors     []string
	PoolTx         string
}

type RoundFinalized struct {
	Id         string
	Txid       string
	ForfeitTxs []string
	Timestamp  int64
}

type RoundFailed struct {
	Id        string
	Err       error
	Timestamp int64
}

type PaymentsRegistered struct {
	Id       string
	Payments []Payment
}

type PaymentsClaimed struct {
	Id       string
	Payments []Payment
}