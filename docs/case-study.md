# ReconPilot case study

*(English first; Türkçe özet aşağıda.)*

## The problem

A mid-size e-commerce operation collects money through more than one pipe at once: card
payments through a PSP, settlements from marketplace platforms, and everything eventually
landing on a bank statement. Each source reports the "same" money in its own way. The PSP
reports the order amount. The marketplace pays out gross minus commission days later, as a
single lump sum for dozens of orders. The bank statement shows whatever actually moved,
whenever it actually moved.

Finance teams close this gap by hand, typically in Excel: export three files, VLOOKUP what
matches, and stare at what doesn't. The hours are one cost. The other is that manual matching
fails silently in two ways. A false match glues two records together because their amounts
happened to coincide, so the books look closed but are wrong. Lost residue is the records that
fall out of every filter and get written off without anyone noticing. Both get worse with volume,
and neither leaves a trace.

## The approach

ReconPilot is a deterministic reconciliation engine written in Go, built correctness-first:

- A four-stage matching chain: exact (reference + amount + direction + ±3-day window), split
  (one order settled by several bank lines that carry its reference), tolerant (bank lines only:
  the same reference within ±0.5 %, or without one the exact amount when no other row of that
  counterparty and amount lies within 30 days), and bounded many-to-one group matching for
  marketplace payouts (payout = Σ orders − commission, ≤20 members, explicit `unknown` fallback
  past the bound).
- The engine does not choose between equal candidates. When two partners fit a record, or two
  sets of orders add up to one payout, the rows stay open as `ambiguous`.
- Eight discrepancy classes for everything left unmatched: commission, refund, partial payment,
  timing shift, duplicate, missing on counterparty side, ambiguous, unknown.
- Three runtime invariants, checked on every run: every input transaction is matched or
  classified; no transaction appears in two match groups; and every transaction-level
  discrepancy delta obeys its type's money semantics. A violation aborts the run, and no partial
  result comes back.
- A fourth guarantee at ingestion: re-ingesting a file adds no transactions. PostgreSQL
  `UNIQUE(dedup_key)` enforces it, and an integration test checks it against a real database.
  Each file is stored in one database transaction, so a failed ingest leaves nothing behind.
- Money is integer kuruş from end to end, with no floats and no rounding drift. The engine
  accepts TRY only and nonnegative amounts; the direction field carries the sign.

## The result

One command (`go run ./cmd/benchmark`) reproduces the claim on about 50,000 synthetic transactions (`-n 50000`)
with eight discrepancy types injected at known positions. The run detects 8/8 injected
discrepancy types and produces 0 false matches; every match is checked member by member against
the generator's intended pairing. It misses 0 intended pairs or groups, and every input record
ends up in a match or a discrepancy. The ~50,000 transactions take about a third of a second on a
laptop.

The run passes for every seed from 1 to 100, at the default size and at 20,000 transactions.

## Operational surface

The same binary now serves a versioned reconciliation endpoint, the HTML report, PostgreSQL-aware
readiness and Prometheus-compatible metrics. `docker compose up --build -d` builds the non-root
container, starts PostgreSQL, loads the golden dataset (loading it again adds no transactions)
and makes the report available on localhost. Matching and classification stay in the pure engine; the
HTTP handlers only call the existing store and engine.

## Benchmark scope

The benchmark data is synthetic and the discrepancies are injected on purpose. The generator
lives in the same repository, and the run is seeded and reproducible. What one command shows is
that the engine detects what it
says it detects, produces no false matches on the generated ground truth, misses no intended
clean pairing, and refuses to return a result if an input record is unplaced or a
transaction-level discrepancy delta breaks its type's rules.

The first version of the generator kept every open row out of every other row's window, and the
benchmark could not see what happened when two rows fitted. Public real data could. On a bank's
ledger against its statement (BenchRec) and on marketplace orders against their payments (Olist)
the old engine made false matches: it treated two missing references as equal, ignored a reference
that named another order, and took the first candidate it found. The rules were rewritten
([ADR 0006](adr/0006-match-only-on-sufficient-evidence.md)), and the generator now injects each of
those cases, so the benchmark fails if one comes back. On two held-out real sets the new engine made
10 false matches of 14,820 and 3 of 6,474, and it left the cases it could not decide open for a
person. [Runs on public real data](real-data.md) has every number and what they do not show.

---

## Türkçe özet

### Problem

Orta ölçekli bir e-ticaret operasyonunda para aynı anda birden çok kanaldan akar: PSP üzerinden
kart tahsilatı, pazaryeri hakedişleri ve hepsinin eninde sonunda düştüğü banka ekstresi. Üç
kaynak "aynı" parayı farklı raporlar. PSP sipariş tutarını gösterir, pazaryeri günler sonra
komisyonu düşülmüş tek bir toplu ödeme yapar, banka ise fiilen ne zaman ne geçtiyse onu yazar.
Finans ekipleri bu farkı çoğunlukla Excel'de elle kapatır. Harcanan saatten daha pahalısı iki
sessiz hatadır. Yanlış eşleşmede tutarları tesadüfen tutan iki kayıt birbirine yapıştırılır;
defter kapanmış görünür ama yanlıştır. Kaybolan bakiye ise hiçbir filtreye takılmayıp kimse fark
etmeden silinen kayıtlardır.

### Yaklaşım

ReconPilot, Go ile yazılmış deterministik bir mutabakat motorudur. Dört aşamalı bir eşleştirme
zinciri (kesin, bölünmüş ödeme, toleranslı ve sınırlı çoktan-bire grup eşleme) ve sekiz uyuşmazlık
sınıfı kullanır. Bir kayda iki aday aynı ölçüde uyuyorsa motor birini seçmez; satırları `ambiguous`
diye işaretler ve kararı bir kişiye bırakır. Her çalışmada üç değişmez kontrol edilir: her girdi işlemi ya eşleşir ya
sınıflandırılır, hiçbir işlem iki eşleşme grubunda yer almaz, işlem bazındaki her fark tutarı
kendi sınıfının para semantiğine uyar. Biri ihlal edilirse motor kısmi sonuç döndürmez. Dördüncü
garanti içe aktarmadadır: aynı dosyayı yeniden yüklemek yeni işlem eklemez. Bunu PostgreSQL
`UNIQUE(dedup_key)` sağlar, gerçek veritabanıyla çalışan bir entegrasyon testi de sınar. Her
dosya tek bir veritabanı transaction'ında kaydedilir; içe aktarma yarıda kalırsa dosyadan hiçbir
şey kalmaz. Para uçtan uca tamsayı kuruştur, float yoktur. Motor yalnızca TRY ve negatif olmayan
tutar kabul eder; işareti yön alanı taşır.

### Sonuç

İddia tek komutla tekrarlanabilir (`go run ./cmd/benchmark`): yaklaşık 50.000 sentetik işlem (`-n 50000`), bilinen
konumlara enjekte edilmiş 8 uyuşmazlık tipi: 8/8 tip tespit, 0 yanlış eşleşme, 0 kaçırılmış
amaçlanan eşleşme/grup. Her girdi kaydı bir eşleşmeye ya da uyuşmazlığa yerleşir ve çalıştırma
dizüstü bilgisayarda saniyenin üçte biri kadar sürer. 1'den 100'e kadar her seed hem varsayılan boyutta
hem 20.000 işlemde geçer.

### Benchmark sınırı

Veri sentetiktir, uyuşmazlıklar bilerek enjekte edilmiştir. Üretici kod aynı depodadır ve
çalıştırma seed'li olduğu için birebir tekrarlanabilir. Gösterdiği şey motorun davranışıdır ve
bunu tek komutla kendiniz doğrulayabilirsiniz.

Üreticinin ilk sürümü her açık satırı diğerlerinin penceresinden uzak tutuyordu ve benchmark
iki satırın aynı anda uyduğu durumu hiç görmedi. Kamuya açık gerçek veri gösterdi: bir
bankanın defteri ile hesap ekstresinde (BenchRec) ve pazaryeri siparişleri ile ödemelerinde (Olist)
eski motor yanlış eşleşmeler yaptı. İki boş referansı eşit saydı, başka bir siparişi gösteren
referansı yok saydı ve bulduğu ilk adayı aldı. Kurallar yeniden yazıldı
([ADR 0006](adr/0006-match-only-on-sufficient-evidence.md)). Üretici artık bu durumların her birini
bilerek ekliyor; biri geri gelirse benchmark düşer. Kurallar yazılırken kullanılmayan iki gerçek sette
yeni motor 14.820 eşleşmede 10, 6.474 eşleşmede 3 yanlış yaptı ve karar veremediği satırları bir
kişiye bıraktı. Bütün sayılar ve sınırları [Runs on public real data](real-data.md) belgesinde.
