# ADR 0006: Match only when the evidence singles out one partner

**Status:** accepted · 2026-10-12

## Context

The first engine was greedy. Each pass walked the open rows in date order and
took the best candidate it found, and a missing reference compared equal to
another missing reference. On the synthetic benchmark that never showed: the
generator kept every open row out of every other row's tolerant window, and
all clean pairs matched exactly. But on public real data it did. On a bank
statement against a ledger with no shared reference (BenchRec), 2,672 of
66,211 matches were false. On marketplace orders against their payments
(Olist), 35 matches paired one order's expected row with another order's
payment, because the tolerant pass ignored a reference that named a different
order. Split payments (voucher plus card) came out as partial, commission or
duplicate.

## Decision

A reference is evidence only when it is there. An empty reference agrees with
nothing, so two unreferenced rows never exact-match and are never duplicates
of each other. A reference identifies a record within one counterparty. If a
row's reference names a record on the other side of the book, the row cannot
pair with anything that carries a different reference.

Without an agreeing reference the engine needs the exact amount, and the pair
must be the only fit: no other open bank line of that counterparty and amount
within `AmountHorizonDays` (30) of the expected row, and no other open expected
row within 30 days of the bank line. The horizon exists because the window can
hide a row's real partner. A paper check that clears eight days late leaves its
own clearance outside the three-day window, while the clearance of an earlier
check of the same vendor and amount lands inside it.

Two kinds of unreferenced pair are never matched. Amounts that only agree
within the 0.5 % tolerance are weaker than an exact amount, so they do not
block a same-amount pair. An unreferenced credit and debit of one amount look
like a reversal, which is as strong as a same-amount pair, so a row with both
kinds of partner is left open. When nothing stronger takes their rows, those
rows are `ambiguous`.

The horizon also bounds classification. Without an agreeing reference a
same-amount row more than 30 days away is no longer a timing difference, and an
expected row with nothing closer on the other side is `missing`.

The group matcher refuses in the same way when two different sets of orders
add up to a payout's gross; the orders of the first two sets it finds become
contested. A later payout whose fitting set uses a contested order is refused
too, because taking the order would settle the first payout by elimination,
and the orders of its own sets become contested in turn. The matcher still
works through payouts in the pool's order (date, then reference): an earlier
payout whose only fitting set takes an order wins it before a later payout is
searched, so two payouts of one day can be decided differently when their
references sort the other way. After the group pass the tolerant pass runs
once more over the open rows, since an order the group pass took no longer
competes for a bank line.

Rows left open on purpose get the discrepancy type `ambiguous`, with the whole
amount as delta, so a person decides them: more than one partner fits, or the
only partner carries too little evidence (a near amount, an unreferenced
reversal).

Two shapes became match kinds of their own: `split`, one expected row settled
by two or more bank lines with its reference that add up exactly, and
`reversal`, a sale and its void under the same reference.

## Consequences

- Precision went up and coverage went down. On the BenchRec evaluation split
  (never used while the rules were written) 10 of 14,820 matches are false
  against the dataset's own allocations, and 37,615 of 69,171 rows are left
  `ambiguous` for review. `docs/real-data.md` has every run.
- A pair whose two real partners both lie beyond the horizon can still be
  matched wrongly. Three such pairs remain on the Fulton County data.
- The group matcher adds positive orders only. When a payout's real set
  includes a credit note, that set is out of reach, and another set of the
  seller's orders can add up to the gross by chance. Two such payouts were
  matched wrongly on the Baltimore checkbook.
- The benchmark generator now injects each shape that the old engine got
  wrong, and unit and property tests pin the individual rules; a rule that
  regresses fails one or the other.
- `split`, `reversal` and `ambiguous` need migration 0002, which widens both
  CHECK constraints and can run on an existing database.
