# Workboard — لوحة المهام الحقيقية

آخر تحديث: 2026-11-23 (المرحلة P0 — التأسيس).

الرموز: ✅ منجز ومختبر · 🟡 منجز جزئيًا · ⬜ لم يبدأ · ⛔ محجوب

---

## P0 — التأسيس والهوية الهندسية

الهدف: مستودع نظيف + خدمة تبني وتعمل وتُختبر + CI جاهز، قبل أي ميزة منتج.

| # | المهمة | الحالة | الدليل |
|---|---|---|---|
| P0-1 | تصحيح عقدة المال (`money.Percent`) وإعادة تعريف الباقي ككسر داخلي | ✅ | `go test ./internal/platform/money -v` → 8 اختبارات ناجحة |
| P0-2 | جعل `DATABASE_URL` اختياريًا في `local` فقط | ✅ | `internal/platform/config/config_test.go` → 7 اختبارات |
| P0-3 | خادم HTTP: routing، request-id، سجلات، استرجاع panic، حدود جسم، رؤوس أمان | ✅ | `internal/httpapi/*` + 7 اختبارات |
| P0-4 | `/healthz`، `/readyz`، `/version`، `/v1/meta` (سياسة المحتوى من مصدر واحد) | ✅ | تشغيل فعلي + curl موثّق في سجل الجلسة |
| P0-5 | هيكل التشغيل: `cmd/api` بإيقاف رشيق وldflags | ✅ | `go build ./...` + إيقاف نظيف عند SIGTERM |
| P0-6 | أول ترحيل قاعدة بيانات: schemas، امتدادات، updated_at، سجل تدقيق hash-chained، outbox، flags | ✅ | طُبِّق على PostgreSQL 18 حقيقي (منفذ 55432) وتحقّقت السلسلة |
| P0-7 | `cmd/migrate` بترحيلات مدمجة (embed) وإعادة تشغيل آمنة | ✅ | تشغيل أول + تشغيل ثانٍ "up to date" |
| P0-8 | Dockerfile متعدد المراحل (distroless, non-root) | 🟡 | مكتوب؛ صورة Docker تُبنى في CI (لا Docker محليًا) |
| P0-9 | تفويض مسار الترحيل (`path.Join` بدل `dir+"/"`) | ✅ | إصلاح خطأ فعلي ظهر عند أول تشغيل |
| P0-10 | هيكل Android: Compose، نظام تصميم، وضع داكن/فاتح، FLAG_SECURE، سياسة محتوى + اختبار وحدة | 🟡 | مكتوب؛ البناء يثبته CI فقط |
| P0-11 | Workflows: backend-ci، android-ci، security، docs | ✅ | تشغيل حقيقي أخضر على `7caa08c` |
| P0-12 | إنشاء المستودع الخاص `jawuu187-ops/content-platform` ودفع `main` | ✅ | `gh repo view` → PRIVATE، 3 commits مدفوعة |
| P0-13 | أول تشغيل ناجح لـ android-ci يُنتج APK + AAB | ✅ | run `45118369901`: APK 11.2MB، AAB 2.1MB، 3 اختبارات JVM، lint 0 أخطاء |
| P0-14 | إصلاح فشل CI الحقيقي (cgo للـrace، وسم Trivy، CodeQL/GHAS، lychee) | ✅ | 4 تشغيلات فاشلة → 4 خضراء |
| P0-15 | تفعيل حماية `main` وCodeQL | ⛔ | تتطلب GitHub Pro/GHAS (مدفوع) — بديل: gosec+staticcheck موثقان |
| P0-16 | سياسة عدم الترقية التلقائية للإصدارات الرئيسية | ✅ | `dependabot.yml` يتجاهل semver-major؛ PRs #2/#3 أُغلقت بالسبب |

## P1 — الهوية والجلسات والأدوار (التالي)

| # | المهمة | الحالة |
|---|---|---|
| P1-1 | جداول `identity`: users, credentials, sessions, devices, mfa_factors, role_grants | ⬜ |
| P1-2 | تسجيل/دخول/استعادة + تحقق بريد وهاتف (محوّلات بريد محلية) | ⬜ |
| P1-3 | TOTP + Passkeys + refresh دوّار مع كشف إعادة الاستخدام | ⬜ |
| P1-4 | RBAC + middleware صلاحيات + اختبار مصفوفة الأدوار | ⬜ |
| P1-5 | حدّ معدل + حماية تعداد الحسابات | ⬜ |
| P1-6 | شاشات Android: دخول، تحقق، إعدادات أجهزة | ⬜ |

---

## سجل الدورة السابقة

| ما أُنجز | ما اختُبر | ما لم يُختبر |
|---|---|---|
| 21 ملفًا (خادم، ترحيل، اختبارات، CI، Android shell) | `go build`, `go vet`, `go test -race`، ترحيل على PG18، سلسلة التدقيق، 4 endpoints | بناء Android، صورة Docker، workflows |

## سجل التحقق — P1 (الالتزام `dab8b38`)

| Workflow | Run | النتيجة | الدليل |
|---|---|---|---|
| Backend CI | `45121217016` | ✅ | 3 حزم اختبار + تكامل على PostgreSQL 18، تغطية إجمالية 62.4%، ترحيل 0001→0003 مرتين، فحص دخان: تسجيل 201 + جلسات 200 + تكرار 409 + مجهول 401 |
| Security | `45121217077` | ✅ | gitleaks، govulncheck، Trivy، gosec، staticcheck (بعد إزالة G404) |

**عيوب حقيقية اكتُشفت وأُصلحت في هذه المرحلة**

| العيب | الأثر | الإصلاح |
|---|---|---|
| `ops.append_audit` يأخذ القفل بعد قراءة السلف | سلسلة التدقيق تتفرّع تحت التزامن فتبدو مخترقة | ترحيل `0003`: القفل أولًا |
| قاعدتا اختبار تتشاركان قاعدة واحدة | كل حزمة تُفرّغ صفوف الأخرى وتتسابق على `schema_migrations` | قاعدة خاصة لكل عملية اختبار |
| `math/rand` في اسم قاعدة الاختبار | gosec G404 | `crypto/rand` |
| إلغاء جلسة ملغاة يعيد 204 | العميل يظن أن جلسة حيّة أُلغيت | شرط `revoked_at IS NULL` |
| اختبار حد المعدل يعتمد على الزمن | فشل متقطع | سطل صغير + اختبارات وحدة بساعة وهمية |

## سجل التحقق على GitHub (2026-11-23)

| Workflow | Run | النتيجة | الدليل |
|---|---|---|---|
| Backend CI | `45118062691` | ✅ | 3 حزم ok، تغطية 84.3/81.8/77.9%، ترحيل مزدوج، `/readyz` = `database:ok` |
| Android CI | `45118369901` | ✅ | APK 11,223,362 بايت (classes.dex)، AAB 2,145,559 بايت (baseline profiles)، 3 اختبارات، lint 0 أخطاء/13 تحذيرًا |
| Security | `45118062649` | ✅ | gitleaks، govulncheck، Trivy، gosec، staticcheck |
| Docs | `45118062700` | ✅ | روابط + بنية |

**ثغرة تغطية معروفة:** `internal/platform/database` و`logging` و`cmd/*` بدون اختبارات (0%). تُعالج في P1 باختبارات تكامل للمُشغّل والسجلات.

## P1 — الهوية والمصادقة

| # | المهمة | الحالة | معيار القبول |
|---|---|---|---|
| P1-1 | ترحيل `0002_identity` (مستخدمون، جلسات، أجهزة، أدوار، تحقق بريد، MFA) | ✅ مُنفَّذ | يُطبَّق مرتين؛ أقفال تمنع إبطال الإلغاء عكسيًا |
| P1-2 | تسجيل/دخول/خروج + PBKDF2-600k + منع تعداد الحسابات | ✅ مُنفَّذ | 401 متطابق للبريد المجهول وكلمة المرور الخاطئة |
| P1-3 | جلسات + refresh دوّار + كشف إعادة الاستخدام | ✅ مُنفَّذ | إعادة استخدام الرمز تُبطل العائلة كاملة |
| P1-4 | RBAC: أدوار صريحة + `HasRole/HasAnyRole` | 🟡 جزئي | المصفوفة الكاملة تحتاج مرحلة الصلاحيات (P1-8) |
| P1-10 | إصلاح عيوب CI الحقيقية: تزامن الترحيل، سلسلة التدقيق، G404 | ✅ | 6 تشغيلات فاشلة → خضراء |
| P1-11 | حدّ معدل مشترك بين النسخ (Redis/Valkey + Lua) | ✅ مُنفَّذ | نسختان مستقلتان تتقاسمان ميزانية واحدة: 10 من 40 طلبًا متزامنًا |
| P1-5 | حدود معدل على نقاط الاعتماد | ✅ مُنفَّذ | 429 + `Retry-After` بعد 10 محاولات |
| P1-6 | تحقق بريد/هاتف بمحوّل بريد محلي | ✅ مُنفَّذ | 8-char code, 30 min TTL, hashed, 5-attempt cap, single use; SMTP sender requires STARTTLS |
| P1-7 | TOTP + Passkeys | ✅ مُنفَّذ | TOTP with RFC 6238 vectors; WebAuthn registration + authentication with replay, relay, tamper, counter and revocation tests |
| P1-8 | مصفوفة صلاحيات كاملة + middleware تفويض | ✅ مُنفَّذ | Every role×permission asserted allowed and denied; HTTP tests prove 403 vs 201 |
| P1-9 | شاشات Android: دخول، تسجيل، أجهزة | ⬜ | Blocked on the visual design approval gate |

## P1 evidence (GitHub Actions only)

| Run | Workflow | Result | Evidence |
| --- | --- | --- | --- |
| 45125154876 | Backend CI | success | coverage total 63.6% (httpapi 79.5%, identity 80.0%); smoke: 401 anonymous, 201 register, 200 sessions, 201 TOTP start, 202 mail request, 403 fan on admin, 409 duplicate |
| 45125154912 | Security | success | gitleaks, govulncheck, Trivy, gosec, staticcheck |

Defects found by CI in this slice and fixed: `mail` import collision with `net/mail`;
pgx.Row assigned to `error`; duplicate test names across files; `email_verified_at`
scanned into a throwaway pointer so `/v1/auth/me` lied; `users.write` with no owning
role; `roles.grant` wrongly required to be non-admin; two integer-overflow
conversions; two dead declarations.

## P1 passkeys and shared rate limiting — evidence

| Run | Workflow | Result | Evidence |
| --- | --- | --- | --- |
| 45128401832 | Backend CI | success | total coverage 63.9% before cross-package attribution (69.7% after, run 45128880770); smoke: passkey registration start 201 with `userVerification:"required"`; Redis service running the shared-limiter tests |
| 45128401922 | Security | success | gitleaks, govulncheck, Trivy, gosec, staticcheck |

Passkey tests (software authenticator, no browser): successful register + login, discoverable
login without an email, wrong challenge, foreign challenge owner, replayed registration and
assertion, tampered signature, relayed origin, relayed RP ID, counter regression, revoked
credential, packed attestation, unsupported format, expired challenge.

Shared limiter tests: two independent clients against one Redis allow exactly 10 of 40
concurrent requests; window expiry restores the budget; capacity boundary; a closed store
returns `ErrUnavailable` and the HTTP wrapper answers 503 rather than allowing traffic.

Defects CI found in this slice and fixed: uint32→uint64 counter assignment, wrong auth-data
field name, `newAuthHandler` call sites, `Result` shape in tests, case-sensitive email
comparison, three gosec integer-narrowing findings, deprecated `ecdsa.PublicKey.X/Y`
construction (staticcheck SA1019), `PublicKey.Bytes` arity.


## P2 — الملفات والأدوار والاشتراكات (creator surface)

| ID | البند | الحالة | الدليل |
| --- | --- | --- | --- |
| P2-1 | مخطط المبدعين: ملف، مستويات، اشتراكات، استحقاقات (`0006_creators.sql`) | ✅ مُنفَّذ | قيود على المال (وحدات صغرى + ISO-4217)، فهرس اشتراك حيّ واحد لكل (مشترك، مبدع)، فهرس استحقاق حيّ واحد لكل نطاق |
| P2-2 | خدمة الملف: تحقق المقبض/الفئة/الطول، مسودة غير قابلة للاستكشاف | ✅ مُنفَّذ | `TestProfileLifecycle` — الملف غير المنشور يُعيد 404 عامًّا |
| P2-3 | المستويات: حدود السعر والعملة، تقاعد بدل حذف، أسماء فريدة بلا حساسية لحالة الأحرف | ✅ مُنفَّذ | `TestTierValidationAndRetirement` + فهرس `lower(name)` |
| P2-4 | الاشتراك والدفع: حالة معلّقة عند غياب مزوّد، تفعيل ذرّي مع الاستحقاق | ✅ مُنفَّذ | `TestSubscriptionGrantsAndRevokesAccess`؛ `ENABLE_FAKE_PAYMENTS` مرفوض في الإنتاج |
| P2-5 | بوابة الوصول: مالك > مدير > عام > استحقاق حيّ > اشتراك داخل الفترة | ✅ مُنفَّذ | `TestPublishingAndOwnership`, `TestDirectGrantScopeAndExpiry`, `TestCancelKeepsAccessUntilPeriodEnd` |
| P2-6 | صلاحية ذاتية لأن تصبح مبدعًا (مسجّلة في التدقيق) | ✅ مُنفَّذ | `identity.BecomeCreator` + التدقيق `role.self_granted` |
| P2-7 | واجهة HTTP: 12 مسارًا مع تفويض بالصلاحيات | ✅ مُنفَّذ | `creators_test.go` ومسار الدخان في CI |
| P2-8 | فحص الجاهزية يقيس المخزن المشترك للحدود | ✅ مُنفَّذ | `TestReadinessFailsWhenTheSharedLimiterIsDown` → 503/degraded، `rate_limit:"unreachable"` |
| P2-9 | شاشات Android للملف/المستويات/الاشتراك | ⬜ | موقوفة على بوابة التصميم البصري |

## P2 evidence (GitHub Actions only)

| Run | Workflow | Result | Evidence |
| --- | --- | --- | --- |
| 45133688918 / 45133688903 | Backend CI | success | total coverage 69.3%; smoke creator journey: create 201 (roles include `creator`), unpublished 404, publish 200, tier 201, public profile 200 with the tier, subscriber register 201, access before subscribing `allowed:false`, subscribe 201, access after `allowed:true`; readiness reported `rate_limit:"in_process"` |
| 45133688xxx | Security | success | gitleaks, govulncheck, Trivy, gosec, staticcheck |

Defects CI found in these slices and fixed: createProfile ignored the publish flag (so every
access check correctly said "not found"); PATCH without `published` would have unpublished a
live profile; a tier created without perks violated NOT NULL; partial tier updates nulled the
perk list; tier uniqueness was case-sensitive; `revokeEntitlementFor` was dead code; the smoke
assertion assumed a role order; gitleaks and dependency-review cannot run on Dependabot PRs
(read-only token, no dependency graph on the free plan).

## P2 — التحقق من العمر (age and identity verification)

| ID | البند | الحالة | الدليل |
| --- | --- | --- | --- |
| P2-A1 | مخطط `verify`: checks/attempts/callbacks/revocations بلا أي عمود للأدلة | ✅ مُنفَّذ | `0007_age_verification.sql`؛ فهرس تحقق حيّ واحد لكل حساب |
| P2-A2 | مزوّد مستقل عن البائع (Start/ParseCallback/Capabilities) + مستقبل HMAC بتوقيت مسموح | ✅ مُنفَّذ | `TestCallbackSignatureAndTimestamp` (توقيع مفقود، جسم معدّل، توقيع قديم) |
| P2-A3 | حظر الوصول قبل التحقق حتى مع اشتراك مدفوع فعّال | ✅ مُنفَّذ | `TestUnverifiedSubscriberCannotAccessPaidContent`, `TestPaidScopeRequiresAgeVerification` |
| P2-A4 | الانتهاء والإلغاء وإعادة التحقق | ✅ مُنفَّذ | `TestExpiredVerificationClosesAccess`, `TestRevocationClosesAccessAndIsAudited` |
| P2-A5 | منع إعادة التشغيل وربط النتيجة بالحساب | ✅ مُنفَّذ | `TestCallbackReplayIsRejected`, `ErrAccountMismatch` |
| P2-A6 | حدود المحاولات | ✅ مُنفَّذ | `TestStartLimitsAndUnavailableProviders` |
| P2-A7 | تقييم المزوّدين (قدرات، خصوصية، تغطية، أسعار، تكامل) | ✅ ورقة تقييم | `docs/vendors/age-verification.md` — لا شراء ولا تعاقد |
| P2-A8 | تحقق فعلي عبر مزوّد مُتعاقد | ⬜ | يحتاج قرار المالك (AV-1..AV-4)؛ حتى ذلك الحين `start` يُعيد 503 والحظر قائم |

## P2 age verification — evidence (GitHub Actions only)

| Run | Workflow | Result | Evidence |
| --- | --- | --- | --- |
| 45161599996 | Backend CI | success | total coverage 69.8%; smoke: subscribed-but-unverified `200` with `age_verification_required`, signed callback `202`, access `200 allowed:true`, status `200 verified:true` |
| 51d971d | Security | success | gitleaks, govulncheck, Trivy, gosec, staticcheck |

Defects CI found in this slice and fixed: the gate field was added to the wrong
file (vet caught it); an unused `insertCheck` binding; a test provider referencing a
removed field; `ops.append_audit` takes `text`, not `uuid`; untyped metadata
parameters in `jsonb_build_object`; a shape constraint that made revocation and
expiry impossible.

## P2 — media posts and protected delivery

| ID | Item | Status | Evidence |
| --- | --- | --- | --- |
| P2-M1 | Upload validation by magic bytes (JPEG/PNG/MP4), size caps, pixel-bomb guard; client `Content-Type` ignored | ✅ implemented | `media/validate_test.go` (`TestIngestAcceptsOnlyRealImagesAndMP4`, `TestIngestRejectsOversizedImage`); smoke: HTML upload answered 415 |
| P2-M2 | Content-addressed storage, no client-supplied path, atomic temp→rename | ✅ implemented | `TestStorageKeysAreHashesAndTraversalFails`, `TestLocalStoreRejectsOversizedStreams` |
| P2-M3 | Replaceable storage seam with fail-closed driver | ✅ implemented | `media.Storage` (`Put`/`Open`/`Stat`), `Unavailable`; `TestUnavailableStorageFailsClosed` |
| P2-M4 | Public / subscriber / PPV visibility on posts, drafts invisible | ✅ implemented | `creator/posts.go`, `0008_media_content.sql`; feed shows `locked` instead of a link |
| P2-M5 | Short-lived single-use signed link, atomic redemption | ✅ implemented | `TestSubscriberMediaIsSingleUseAndRechecked`, `TestConcurrentRedemptionOnlyOneWins` |
| P2-M6 | Age gate + entitlement re-checked at redemption | ✅ implemented | `TestRevocationAndCancellationCloseIssuedLinks`, `TestPPVMediaNeedsAnUnlock` |
| P2-M7 | No bypass by direct URL, foreign media pairing, or unknown token | ✅ implemented | `TestMediaCannotBePairedWithAnotherPost`, `TestUnknownTokenIsRefused`, tampered signature case |
| P2-M8 | Object storage / CDN selection | ⬜ pending owner decision (zero-cost) | `docs/vendors/media-storage.md` (MS-1..MS-3) |
| P2-M9 | Transcoding, thumbnails, `Range` requests, scheduled token sweep | ⬜ not implemented | documented as limitations |

### P2 media — evidence (GitHub Actions only)

| Run | Workflow | Result | Evidence |
| --- | --- | --- | --- |
| _pending_ | Backend CI | — | — |
| _pending_ | Security | — | — |
