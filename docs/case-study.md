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
  payouts (payout = Σ orders − commission, ≤20 members, honest `unknown` fallback past the
  bound).
- **Seven discrepancy classes** for everything unmatched: commission, refund, partial payment,
  timing shift, duplicate, missing on counterparty side, unknown.
- **Four invariants** that turn "the books balance" from a hope into a checked property on
  every run: no kuruş unaccounted for; no transaction in two matches (database `UNIQUE`, not
  just code); classified deltas sum exactly to the reported difference; re-ingesting a file is
  a no-op. A violation aborts the run — the engine cannot produce a wrong report silently.
- **Money is integer kuruş end-to-end.** No floats, no epsilons, no rounding drift.

## The result

The claim is reproducible with one command (`go run ./cmd/benchmark`), against ~50,000
synthetic transactions with seven discrepancy types injected at known positions:

- **7/7 injected discrepancy types detected**
- **0 false matches** — measured by validating every produced match member-by-member against
  the generator's intended-pairing ground truth, not asserted
- **Zero kuruş imbalance**, checked by the invariant suite inside the run
- ~49,000 transactions reconciled in **~4 seconds** on a laptop

## Honest methodology note

The benchmark data is synthetic and the discrepancies are deliberately injected; the generator
lives in the same repository and the run is seeded and reproducible. This project does not
claim production mileage on real customer data. It claims something a prospective client can
verify in one command: the engine detects what it says it detects, produces no false matches
on the adversarial cases it was designed for, and structurally cannot lose money in the books.

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
eşleştirme zinciri (kesin → toleranslı → sınırlandırılmış çoktan-bire grup eşleme), yedi
uyuşmazlık sınıfı ve her çalışmada zorunlu dört değişmez (invariant) — hiçbir kuruş açıkta
kalamaz, hiçbir işlem iki eşleşmede olamaz (veritabanı `UNIQUE` kısıtı), sınıflandırılmış
farkların toplamı rapor edilen toplam farka eşittir, aynı dosyayı iki kez yüklemek etkisizdir.
İhlal durumunda motor rapor üretmez, hata verir: **sessizce yanlış rapor üretmek yapısal
olarak imkânsızdır.** Para uçtan uca tamsayı kuruştur; float yoktur.

**Sonuç.** İddia tek komutla tekrarlanabilir (`go run ./cmd/benchmark`): ~50.000 sentetik
işlem, bilinen konumlara enjekte edilmiş 7 uyuşmazlık tipi → **7/7 tip tespit, ölçülmüş 0
yanlış eşleşme, sıfır kuruş açığı**, dizüstü bilgisayarda ~4 saniye.

**Dürüstlük notu.** Veri sentetiktir, uyuşmazlıklar bilerek enjekte edilmiştir; üretici kod
aynı depodadır ve çalıştırma tohumlu (seeded) olduğu için birebir tekrarlanabilir. İddia
"gerçek müşteri verisinde çalıştı" değil, "motorun davranışı doğrulanabilir ve bunu tek
komutla kendiniz doğrulayabilirsiniz" iddiasıdır.
