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

- A three-stage matching chain: exact (reference + amount + direction + ±3-day window),
  tolerant (±0.5%, bank lines only), and bounded many-to-one group matching for marketplace
  payouts (payout = Σ orders − commission, ≤20 members, explicit `unknown` fallback past the
  bound).
- Seven discrepancy classes for everything left unmatched: commission, refund, partial payment,
  timing shift, duplicate, missing on counterparty side, unknown.
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

One command (`go run ./cmd/benchmark`) reproduces the claim on about 49,000 synthetic transactions (`-n 50000`)
with seven discrepancy types injected at known positions. The run detects 7/7 injected
discrepancy types and produces 0 false matches; every match is checked member by member against
the generator's intended pairing. It misses 0 intended pairs or groups, and every input record
ends up in a match or a discrepancy. The ~49,000 transactions take about one second on a laptop.

The run passes for every seed from 1 to 100, at the default size and at 20,000 transactions.

## Operational surface

The same binary now serves a versioned reconciliation endpoint, the HTML report, PostgreSQL-aware
readiness and Prometheus-compatible metrics. `docker compose up --build -d` builds the non-root
container, starts PostgreSQL, loads the golden dataset (loading it again adds no transactions)
and makes the report available on localhost. Matching and classification stay in the pure engine; the
HTTP handlers only call the existing store and engine.

## Benchmark scope

The benchmark data is synthetic and the discrepancies are injected on purpose. The generator
lives in the same repository, and the run is seeded and reproducible. There is no production use
on real customer data behind it. What one command does show is that the engine detects what it
says it detects, produces no false matches on the generated ground truth, misses no intended
clean pairing, and refuses to return a result if an input record is unplaced or a
transaction-level discrepancy delta breaks its type's rules.

The generator keeps its ground truth unambiguous, and that is a design choice of the synthetic
data. The open rows of an injected discrepancy never share a tolerant window with an open row of
another injected order of the same seller; clean orders are matched exactly before the tolerant
pass. A seller paid in groups gets at most two payouts, and the second group is shifted 35 days
later, so the two payout windows never overlap (the payout dates end up 16 to 54 days apart).
Real books can contain ambiguous same-seller rows that the tolerant or group matcher would pair, and the
benchmark does not measure that case.

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

ReconPilot, Go ile yazılmış deterministik bir mutabakat motorudur. Üç aşamalı bir eşleştirme
zinciri (kesin → toleranslı → sınırlandırılmış çoktan-bire grup eşleme) ve yedi uyuşmazlık sınıfı
kullanır. Her çalışmada üç değişmez kontrol edilir: her girdi işlemi ya eşleşir ya
sınıflandırılır, hiçbir işlem iki eşleşme grubunda yer almaz, işlem bazındaki her fark tutarı
kendi sınıfının para semantiğine uyar. Biri ihlal edilirse motor kısmi sonuç döndürmez. Dördüncü
garanti içe aktarmadadır: aynı dosyayı yeniden yüklemek yeni işlem eklemez. Bunu PostgreSQL
`UNIQUE(dedup_key)` sağlar, gerçek veritabanıyla çalışan bir entegrasyon testi de sınar. Her
dosya tek bir veritabanı transaction'ında kaydedilir; içe aktarma yarıda kalırsa dosyadan hiçbir
şey kalmaz. Para uçtan uca tamsayı kuruştur, float yoktur. Motor yalnızca TRY ve negatif olmayan
tutar kabul eder; işareti yön alanı taşır.

### Sonuç

İddia tek komutla tekrarlanabilir (`go run ./cmd/benchmark`): yaklaşık 49.000 sentetik işlem (`-n 50000`), bilinen
konumlara enjekte edilmiş 7 uyuşmazlık tipi: 7/7 tip tespit, 0 yanlış eşleşme, 0 kaçırılmış
amaçlanan eşleşme/grup. Her girdi kaydı bir eşleşmeye ya da uyuşmazlığa yerleşir ve çalıştırma
dizüstü bilgisayarda yaklaşık bir saniye sürer. 1'den 100'e kadar her seed hem varsayılan boyutta
hem 20.000 işlemde geçer.

### Benchmark sınırı

Veri sentetiktir, uyuşmazlıklar bilerek enjekte edilmiştir. Üretici kod aynı depodadır ve
çalıştırma seed'li olduğu için birebir tekrarlanabilir. Arkasında gerçek müşteri verisiyle üretim
kullanımı yok. Gösterdiği şey motorun davranışıdır ve bunu tek komutla kendiniz
doğrulayabilirsiniz.

Üretici doğru cevap kümesini tek anlamlı tutar; bu, sentetik verinin bilinçli bir tasarım
tercihidir. Enjekte edilen bir uyuşmazlığın açık satırları, aynı satıcının enjekte edilmiş başka
bir siparişinin açık satırlarıyla hiçbir zaman aynı tolerans penceresine düşmez; temiz siparişler
toleranslı eşleştirmeden önce birebir eşleşir. Grup halinde ödeme alan bir satıcıya en fazla iki
ödeme yapılır ve ikinci grup 35 gün sonraya kaydırılır, böylece iki ödeme penceresi çakışmaz (iki
ödeme tarihi arasında 16 ile 54 gün olur). Gerçek defterlerde toleranslı ya da grup eşleştiricinin birbirine bağlayacağı, aynı satıcıya ait
belirsiz satırlar olabilir. Benchmark bu durumu ölçmez.
