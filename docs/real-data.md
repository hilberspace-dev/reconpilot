# Runs on public real data

The benchmark in the README is synthetic. To see how the engine behaves on books it was not built for,
it was run on five public datasets that carry their own ground truth: which rows the institution
linked. None of the data is in this repository.

## How the numbers are scored

`cmd/realrun` reads the three source CSVs through the normal adapters, runs `engine.Run` in memory and
scores the result against a truth file (`source,row,label,type`). Rows that share a label belong
together. A match is false when its rows do not all carry the same label, and precision is the share
of matches that are not false. Coverage is the share of
truth groups with two or more rows that ended up whole inside one match. Rows the engine leaves open
are not errors; `ambiguous` rows are the ones it refused to decide.

```sh
go run ./cmd/realrun -psp psp.csv -bank bank.csv -truth truth.csv -out run-dir
go run ./cmd/realrun -psp psp.csv -marketplace marketplace.csv -truth truth.csv -out run-dir
```

A bank row that the source prints without a reference keeps an empty reference.

## Results

Old is commit `a9bd8dd`, new is the engine after ADR 0006. Both ran on the same converted files.

| Data | Role | Rows | Old: matches, false, precision | New: matches, false, precision | New: ambiguous rows | New coverage |
|---|---|---|---|---|---|---|
| Olist orders vs payments | development | 202,552 | 93,682, 35, 99.96 % | 96,522, 0, 100 % | 0 | 96,522 / 98,665 |
| BenchRec train, ledger vs statement | development | 149,854 | 66,211, 2,672, 95.96 % | 35,478, 105, 99.70 % | 72,557 | 35,342 / 55,788 |
| BenchRec train, one-to-one groups only | development | 94,048 | 46,813, 1,056, 97.74 % | 36,342, 0, 100 % | 21,059 | 36,342 / 47,024 |
| BenchRec evaluation split | held out | 69,171 | 31,187, 1,360, 95.64 % | 14,820, 10, 99.93 % | 37,615 | 11,783 / 18,465 |
| Fulton County checks vs clearance, no check number on the bank side | held out | 139,279 | 29,647, 479, 98.38 % | 26,320, 3, 99.99 % | 6,710 | 26,317 / 67,643 |
| Fulton County, check number on the bank side | held out | 139,279 | 29,647, 469, 98.42 % | 29,178, 0, 100 % | 0 | 29,178 / 67,643 |
| Brazil federal accruals vs payments | held out | 172,002 | 13,320, 5,920, 55.56 % | 6,474, 3, 99.95 % | 5,692 | 4,942 / 47,999 |
| Baltimore checkbook, invoices vs checks | held out | 520,943 | did not finish in 30 minutes | 116,089, 2, 99.998 % | 4,274 | 116,061 / 130,760 |

The development sets were used to find the faults. The others were converted only after the rules
were written. Two of them still changed the engine, and the numbers before those changes are kept
here:

- Fulton County showed that the window can hide a row's real partner and that check numbers repeat
  across vendors. That gave the 30-day horizon and the counterparty scope of references. Before them the
  BenchRec evaluation split had 22 false matches of 16,528 and Fulton County (no check number) 387 of
  29,302.
- Baltimore ran first with 2 false matches of 116,099. A change made after a code review (orders of an
  ambiguous payout held back from later payouts) raised that to 20, because a later payout whose own
  set held such an order then found a different set that added up by chance. The final engine refuses
  that payout instead and is back at 2 of 116,089.

The Brazil set influenced no rule.

Coverage is low where the data has no shared reference. Without one the engine needs the exact amount
and no rival within 30 days, and it leaves everything else open.

## The datasets

### Olist

Brazilian marketplace orders and their payments, anonymised by the publisher (Kaggle
`olistbr/brazilian-ecommerce`, CC BY-NC-SA 4.0). Expected side: one row per order with items, amount =
Σ(price + freight), dated by purchase. Actual side: one row per payment row, dated by the order's
approval. Both sides carry the order id as reference and the seller as counterparty. Truth: the order
id. The old engine paired 35 orders with another order's payment of the same seller; the new one
refuses because the payment names a different order. Orders paid by voucher plus card (2,910) were
typed partial, commission or duplicate. 2,900 of them are now `split` matches. The parts of the other
10 arrived more than 3 days after the purchase and are all `timing`.

### BenchRec

A bank's cash reconciliation, bank statement against internal ledger, one USD account, from the ICAIF
2023 challenge (Kaggle `benchmarkteam/benchrec-real-world-cash-reconciliation-dataset`, CC BY 4.0). The
card states label errors of about 0.2 %. Ledger rows are the expected side, statement rows the actual
side; the sign of the amount gives the direction. The statement prints no reference the ledger shares,
so both sides have empty references. Truth on the train file: `matchId`. Truth on the evaluation split:
the statement row's target allocations from the solution file, with each statement row and the ledger
rows it targets joined into one group (on the train file this scoring gives 90 false matches where
`matchId` gives 105).

All remaining false matches pair a ledger row and a statement row with exactly the same amount and
direction, 0 to 3 days apart, with no other candidate. None of them swaps two one-to-one pairs: the
institution placed one or both rows in a many-row group or left them unassigned. Amount and date cannot tell these apart; the description text
might, and the engine does not read it.

### Fulton County, Georgia: vendor payments

The county's vendor payment register (data.fultoncountyga.gov, dataset `mxhc-krcg`, CC BY 4.0 in its
metadata), 228,425 lines from 2022 to 2026. Expected side: one row per check number and vendor,
excluding cancelled checks, dated by disbursement. Actual side: one row per paid check, dated by its
clearance. Counterparty: the vendor code. Truth: check number and vendor. Two variants: the bank row
carries the check number, as a statement prints it, or nothing.

With the check number every match is exact and correct; checks that clear more than 3 days after
issue are `timing`, and the 3,993 checks never cleared are `missing`. Without it the dates decide:
paper checks clear a median of 7 days after issue (95 % later than 3 days), so a check's own clearance
is often outside the window while an older check of the same vendor and amount clears inside it. That
is what the horizon rule stops. In the 3 remaining false matches the bank row's own check was issued
39 to 70 days earlier, beyond the horizon, and the ledger row's own clearance came 33 or 56 days later
or not at all.
Check numbers are reused across vendors, one number on as many as 42. The engine treats a reference
as naming a record within its counterparty only.

### Brazil: federal expenditure (Portal da Transparência)

SIAFI data published by the CGU (portaldatransparencia.gov.br, open data under Decree 8.777/2016).
Cohort: every commitment (empenho) issued in March 2025, followed to 31 May 2025. Expected side: one row
per accrual document (liquidação) on those commitments. Actual side: one row per cash payment document
(OB, DF, DR, GP, GR, GF) that pays only cohort commitments. Payroll and personnel commitments, documents
present in both files, and every row whose payee is not a company id (CNPJ) are dropped, which removes
natural persons; payee ids are replaced by a salted hash. No code is shared between the two sides, so
both have empty references. Truth: connected groups of documents and the commitments they cite.

Many accruals are settled by a net transfer to the supplier plus separate tax documents, often weeks
later. Without a shared reference those cannot be matched, and they stay open. The 3 false matches pair
an accrual and a payment of the same supplier and amount, 2 or 3 days apart. In the institution's
records one of those payments belongs to an accrual registered under another CNPJ, one is part of the
split payment of a larger accrual, and one belongs to an accrual that is not in the converted book.

### Baltimore: city checkbook

Baltimore's open checkbook from FY2022 (City Code § 9-8 publishes it under an open licence that allows
commercial use). Each row is one supplier invoice paid by a check, and the check's total is published
on its own. Expected side: one row per invoice line, dated by the payment date, the only date the file
has. Payout side: one row per check, gross = the published total, no commission. Counterparty: a salted
hash of the supplier name. Truth: the check number. This is the only set that exercises the group
matcher.

18,112 checks have more than 20 candidate invoices of their supplier in the 14-day window, so they are
not searched and stay `unknown`, and their invoices stay open. The 2 false matches are checks whose real
invoice set contains a credit note. The group matcher adds positive invoices only, so the real set is out
of reach, and another set of the same supplier's invoices adds up to the check total by chance.

## What these runs do not show

- The sets come from a bank, a marketplace and public bodies. PSP settlement files and Turkish bank
  statement formats are not among them.
- Truth comes from the publishers. BenchRec states its own label error rate; the others state none.
- The converters are not in this repository; the mappings above are the whole of what they do.
