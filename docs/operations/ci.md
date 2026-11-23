# CI/CD — ما يعمل فعليًا، وما لا يعمل، ولماذا

آخر تحقق حقيقي: 2026-11-23 على المستودع `jawuu187-ops/content-platform` (private).

## 1. الحالة الفعلية للـworkflows

| Workflow | المشغّل | الحالة الحقيقية |
|---|---|---|
| `backend-ci.yml` | push/PR على `backend/**` + يدوي | أخضر: gofmt، vet، اختبارات `-race` على PostgreSQL 18 خدمي، ترحيل مزدوج، smoke test، بناء API + migrate، صورة Docker |
| `android-ci.yml` | push/PR على `android/**` + يدوي | أخضر: `assembleDebug` + `testDebugUnitTest` + `lintDebug` + `bundleRelease`؛ artifacts: APK وAAB وreports |
| `security.yml` | push، PR، أسبوعيًا | gitleaks + govulncheck + Trivy + gosec + staticcheck |
| `docs.yml` | push/PR على الوثائق + يدوي | فحص روابط (offline) + منع tabs/whitespace |
| `release.yml` | لم يُنشأ بعد | يُضاف في مرحلة P11 |

## 2. قيود خطة GitHub (ليست أخطاء في الكود)

| الميزة المطلوبة | الوضع | السبب | البديل المستخدم |
|---|---|---|---|
| حماية فرع `main` | **غير متاحة** | تتطلب GitHub Pro أو مستودعًا عامًا؛ الخطة المجانية ترفض واجهة API للحماية على المستودعات الخاصة | فرع `main` خطي، وكل تغيير عبر commit موقّع الهوية؛ تُفعَّل الحماية فور الترقية |
| CodeQL code scanning | **غير متاح** | يتطلب GitHub Advanced Security للمستودعات الخاصة | gosec + staticcheck + govulncheck + CodeQL قابل لإعادة التفعيل (أدناه) |
| Secret scanning (push protection) | غير متاح | ميزة مدفوعة للمستودعات الخاصة | gitleaks على كل push/PR + فحص يدوي قبل الدفع |
| Dependabot alerts/updates | **مفعّل** | مجاني | يعمل على Go وGradle وactions وDocker |

إعادة تفعيل CodeQL عند الترقية: أضف هذا الـjob إلى `security.yml`:

```yaml
  codeql:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      security-events: write
    steps:
      - uses: actions/checkout@v4
      - uses: github/codeql-action/init@v3
        with: { languages: go, queries: security-extended }
      - uses: github/codeql-action/autobuild@v3
        with: { working-directory: backend }
      - uses: github/codeql-action/analyze@v3
```

## 3. الإصدارات المثبّتة (لا ترقيات تلقائية)

| المكوّن | الإصدار | سبب الاختيار |
|---|---|---|
| Go | 1.27 (من `backend/go.mod`) | متاح ومستقر، وأدوات التحليل تدعمه |
| PostgreSQL | 18-alpine (خدمة CI) | توافق `pgcrypto`/`citext` وpartitioning |
| pgx | v5.13.0 | أحدث خط 5.x مستقر، بلا CGO |
| Gradle | 8.14.3 (عبر `gradle/actions/setup-gradle`) | متوافق مع AGP 8.13.x |
| AGP | 8.13.2 | آخر خط 8.x مستقر (خط 9.x يغيّر DSL ويحتاج Gradle 9) |
| Kotlin | 2.2.20 | متوافق مع AGP 8.13 وCompose BOM المثبّت |
| Compose BOM | 2026.05.01 | مستقر ولا يتطلب `compileSdk 37.1` |
| compileSdk / targetSdk | 36 / 36 | متوفر على runner؛ 37.1 يخص Compose 1.13 وهو ترقية لاحقة |
| Java | Temurin 21 | LTS مدعوم من AGP 8.13 |
| gosec | v2.30.0 | مثبّت في الأمر نفسه |
| staticcheck | v0.8.2 (= 2026.2.2) | مثبّت في الأمر نفسه |
| Trivy action | v0.36.0 | وسّم موجود؛ 0.28.0 كان وسْمًا غير موجود + نسخة عليها تحذير أمني |

**قاعدة:** لا ترقية لإصدار رئيسي جديد (AGP 9، Compose يتطلب SDK جديد) إلا بعد نجاح بناء في CI
على فرع منفصل، مع تسجيل السبب في `docs/decisions-log.md`.

## 4. الأدلة والتحقق

- كل تشغيل ينتج artifacts قابلة للتنزيل (APK/AAB/تقارير/تغطية/SARIF).
- فشل أي workflow يُصلَح في الـworkflow أو الكود، **لا** بنقل البناء إلى Kali أو VPS.
- البناء المحلي لـAndroid ممنوع تمامًا: لا SDK ولا Gradle على الجهاز.

## 5. تكلفة التشغيل

لا مدفوعات: المستودع خاص على الخطة المجانية. الاستهلاك يُراقب من
`Settings → Billing → Actions`. إذا تجاوز الاستهلاك الحد المجاني، يُخفَّض عدد التشغيلات
(دمج الفحوص في workflow واحد) لا تُشترى دقائق دون موافقة المالك.
