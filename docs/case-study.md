# Case Study — ReconPilot

*(English first; Türkçe özet aşağıda.)*

## The problem

A mid-size e-commerce operation collects money through more than one pipe at once: card
payments through a PSP, settlements from marketplace platforms, and everything eventually
landing on a bank statement. Each source reports the "same" money differently — the PSP
reports the order amount, the marketplace pays out gross minus commission days later as a
single lump sum for dozens of orders, and the bank statement shows whatever actually moved,
whenever it actually moved.

Finance teams close this gap by hand, typically in Excel: export three files, VLOOKUP what
matches, and stare at what doesn't. The cost is not only the hours. Manual matching has two
silent failure modes: **false matches** (two records glued together because the amounts
happened to coincide — the books look closed, but they are wrong) and **lost residue**
(records that fall out of every filter and are quietly written off). Both get worse
super-linearly with volume, and neither leaves a trace.

## The approach

ReconPilot is a deterministic reconciliation engine written in Go, deliberately built
correctness-first:

- A **three-stage matching chain** — exact (reference + amount + direction + ±3-day window),
  tolerant (±0.5%, bank lines only), and bounded many-to-one group matching for marketplace
  payouts (payout = Σ orders − commission, ≤20 members, explicit `unknown` fallback past the
  bound).
- **Seven discrepancy classes** for everything unmatched: commission, refund, partial payment,
  timing shift, duplicate, missing on counterparty side, unknown.
- **Three runtime invariants** on every run: every input transaction is matched or classified;
  no transaction appears in two match groups; and every transaction-level discrepancy delta obeys
  its type's money semantics. A violation aborts the run rather than returning a partial result.
- **A fourth, ingestion-level guarantee:** re-ingesting a file is a no-op, enforced by PostgreSQL
  `UNIQUE(dedup_key)` and verified by a real-database integration test.
- **Money is integer kuruş end-to-end.** No floats, no epsilons, no rounding drift.

## The result

The claim is reproducible with one command (`go run ./cmd/benchmark`), against ~50,000
synthetic transactions with seven discrepancy types injected at known positions:

- **7/7 injected discrepancy types detected**
- **0 false matches** — every produced match was validated member-by-member against the
  generator's intended-pairing ground truth
- **0 intended pairs/groups missed**, with every input record placed in a match or discrepancy
- ~49,000 transactions reconciled in **~4 seconds** on a laptop

## Operational surface

The same binary now serves a versioned reconciliation endpoint, the HTML report, PostgreSQL-aware
readiness and Prometheus-compatible metrics. `docker compose up --build -d` builds the non-root
container, starts PostgreSQL, idempotently loads the golden dataset and makes the report available
on localhost. Matching and classification remain in the pure engine; HTTP handlers only orchestrate
the existing store and engine boundaries.

## Benchmark scope

The benchmark data is synthetic and the discrepancies are deliberately injected; the generator
lives in the same repository and the run is seeded and reproducible. This project does not
claim production mileage on real customer data. It claims something a prospective client can
verify in one command: the engine detects what it says it detects, produces no false matches
on the generated ground truth, misses no intended clean pairing, and refuses to return a result
if an input record is unplaced or a transaction-level discrepancy delta violates its type rules.

---

## Türkçe Özet

**Problem.** Orta ölçekli bir e-ticaret operasyonunda para aynı anda birden çok kanaldan akar:
PSP üzerinden kart tahsilatı, pazaryeri hakedişleri ve hepsinin eninde sonunda düştüğü banka
ekstresi. Üç kaynak "aynı" parayı farklı raporlar: PSP sipariş tutarını, pazaryeri günler
sonra komisyon düşülmüş tek bir toplu ödemeyi, banka ise fiilen ne zaman ne geçtiyse onu.
Finans ekipleri bu farkı çoğunlukla Excel'de elle kapatır. Asıl maliyet saat değil, iki sessiz
hata türüdür: **yanlış eşleşme** (tutarları tesadüfen tutan iki kaydın yapıştırılması — defter
kapanmış görünür ama yanlıştır) ve **kaybolan bakiye** (hiçbir filtreye takılmayıp sessizce
silinen kayıtlar).

**Yaklaşım.** ReconPilot, Go ile yazılmış deterministik bir mutabakat motorudur: üç aşamalı
eşleştirme zinciri (kesin → toleranslı → sınırlandırılmış çoktan-bire grup eşleme) ve yedi
uyuşmazlık sınıfı kullanır. Her çalışmada üç değişmez zorlanır: her girdi işlemi eşleştirilir
veya sınıflandırılır; hiçbir işlem iki eşleşme grubunda yer alamaz; işlem bazındaki her fark
tutarı kendi sınıfının para semantiğine uyar. İhlal durumunda motor kısmi sonuç döndürmez.
Dördüncü, içe aktarma düzeyindeki garanti aynı dosyanın yeniden yüklenmesini etkisiz kılar;
PostgreSQL `UNIQUE(dedup_key)` ve gerçek veritabanı entegrasyon testiyle doğrulanır. Para uçtan
uca tamsayı kuruştur; float yoktur.

**Sonuç.** İddia tek komutla tekrarlanabilir (`go run ./cmd/benchmark`): ~50.000 sentetik
işlem, bilinen konumlara enjekte edilmiş 7 uyuşmazlık tipi → **7/7 tip tespit, 0 yanlış
eşleşme, 0 kaçırılmış amaçlanan eşleşme/grup**; her girdi kaydı bir eşleşmeye veya uyuşmazlığa
yerleştirilmiş halde, dizüstü bilgisayarda ~4 saniye.

**Benchmark sınırı.** Veri sentetiktir, uyuşmazlıklar bilerek enjekte edilmiştir; üretici kod
aynı depodadır ve çalıştırma tohumlu (seeded) olduğu için birebir tekrarlanabilir. İddia
"gerçek müşteri verisinde çalıştı" değil, "motorun davranışı doğrulanabilir ve bunu tek
komutla kendiniz doğrulayabilirsiniz" iddiasıdır.
