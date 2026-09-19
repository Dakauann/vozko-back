package balance

// MinAIFloorMicros is the balance below which no AI call is made, anywhere.
//
// It lives in the domain because it is a policy about the customer's money, not
// an implementation detail of whichever feature happens to be about to spend
// it. It was written out as a local `minBalanceFloor int64 = 10_000` in four
// separate use cases before this, which meant four chances for them to drift
// and no single place to change the number.
//
// Micros, matching every other amount in this package: a millionth of the
// account currency, so 10.000 micros is one cent.
const MinAIFloorMicros int64 = 10_000
