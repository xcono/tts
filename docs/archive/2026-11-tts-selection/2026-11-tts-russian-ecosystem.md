# Русскоязычная TTS-экосистема: что нового с апреля по ноябрь 2026

Дата сбора: 2026-11-17. Трек: модели, сделанные специально под русский, и инструменты
для ударений, нормализации и английских терминов в русском тексте. Ничего не
устанавливалось, веса не скачивались. Источники: карточки HF (API `huggingface.co/api/models`),
GitHub (`gh api`), Habr, r/LocalLLaMA, Brave Search.

**Обозначения:** **[П]** — подтверждено карточкой модели, кодом, релизом или issue;
**[З]** — заявлено автором или третьей стороной, нами не проверено; **[?]** — неизвестно.

Контекст задачи: русская речь с английскими IT-терминами, максимум качества, 2× RTX 5060 Ti
16 ГБ (sm_120, нужен torch cu128+). Сейчас установлен F5-TTS с русским fine-tune (термины
приходится писать кириллицей). Qwen3-TTS 0.6B-Base звучит хорошо, но ударениями не управляет.

---

## Главное (TL;DR)

1. **Главная новость периода — Qwen3-TTS 1.7B-Base, дообученная слушаться знака ударения U+0301.**
   [`siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf`](https://huggingface.co/siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf)
   (2026-10-04, Apache-2.0). По их детектору ставит ударение по знаку в 100% случаев, когда знак
   совпадает со словарём, и в **96%**, когда знак идёт против словаря (у базовой модели 17%).
   Это закрывает ровно тот недостаток Qwen3-TTS, из-за которого в апреле выбрали F5.
   Источник рецепта — [olluorg/voicy](https://github.com/olluorg/voicy), локальный сервер с
   OpenAI-совместимым API, который **исследует именно озвучку инженерных текстов** на русском.
2. **Ещё два fine-tune'а с управляемым ударением на сильных мультиязычных базах:**
   [Shtorm-VoxCPM2-RU](https://huggingface.co/ArtShtorm/Shtorm-VoxCPM2-RU) (VoxCPM2 2.3B, `+`)
   и [FireRedTTS3-Base-Russian](https://huggingface.co/shustov-te/FireRedTTS3-Base-Russian) (без
   разметки ударений). Обе без формальных замеров.
3. **Акцентуаторы обновились:** silero-stress v1.5 (2026-09-15, 2 208 омографов, MIT), RUAccent
   сменил лицензию на MIT (2026-07-17), появились форки с дообученным резолвером «ё» и порт на Rust.
4. **Нормализатор с кириллизацией латиницы:** [ru-normalizr](https://github.com/NickZaitsev/ru-normalizr)
   v1.3.0 (2026-10-12, MIT): латиница → кириллица через IPA-бэкенд, по желанию со знаками ударения.
   Это единственный новый пункт в awesome-russian-speech за период.
5. **Латиница в русской фразе по-прежнему ненадёжна** ни в одной модели, у которой это измеряли:
   voicy на Qwen3-TTS показал, что одно и то же слово читается по-разному в зависимости от фразы.
   Рабочая практика 2026 года — **словарь терминов → кириллица + знак ударения**, а не «модель сама
   прочитает». Эту часть текущей схемы менять не нужно, её нужно автоматизировать.
6. **У F5-TTS русских fine-tune'ов после апреля ничего существенного нет:** у Misha24-10 последний
   чекпойнт `v4_winter` от 2026-01-13, у ESpeech новых TTS-весов нет с августа 2025.

---

## (а) Сводная таблица моделей

Колонка «Ударение» — способ явно задать ударение. «EN» — поведение на латинице в русской фразе.
VRAM — оценка или замер из источника, не наш.

| Модель | Дата | Размер / VRAM | Лицензия | Ударение | EN-вставки | Голос | Стриминг / скорость | sm_120 | OpenAI-сервер | Демо |
|---|---|---|---|---|---|---|---|---|---|---|
| **[Qwen3-TTS 1.7B ru-stress-cf](https://huggingface.co/siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf)** (siriusfreak) | 2026-10-04 | 1.7B; bf16 ≈ 9 ГБ¹; GGUF Q4 1.1 ГБ / ~1.4 ГБ VRAM² | Apache-2.0 (⚠ часть данных без лицензии) | **U+0301 после гласной, 96% против словаря [П]** | ⚠ обучали только на русском; латиница не измерена [?] | клон 3 с + транскрипт | AR, стриминг; RTF ≈0.2 (GGUF, 3060)² | torch-путь = тот же `qwen_tts`, что уже работает с 0.6B; ⚠ 1.7B раньше вешал GPU1³ | vLLM-Omni [З]; voicy (своя GGUF-сборка) [П] | [до/после](https://olluorg.github.io/voicy/stress/) (voicy-версия) |
| [Qwen3-TTS 1.7B ru-stress (voicy)](https://huggingface.co/sknyazev/qwen3-tts-12hz-1.7b-ru-stress-gguf) (sknyazev) | 2026-09-24 | GGUF q5_k 1.0 ГБ + ONNX | Apache-2.0 | U+0301, 77% против словаря (по замеру siriusfreak) [П] | термины кириллицей + словарь voicy [П] | клон 8–14 с, 2 готовых | ×3.5 RT на 3080 [З] | llama.cpp/ONNX, без torch [П] | **voicy, OpenAI-совместимый [П]** | то же |
| [Qwen3-TTS 1.7B RU pruned Q4_K_M](https://huggingface.co/Igor282/qwen3-tts-12hz-1.7b-ru-pruned-enru-Q4_K_M) (Igor282) | 2026-11-16 | 1.11 ГБ, ~1.4 ГБ VRAM | Apache-2.0 | U+0301 (это siriusfreak с урезанным словарём) | словарь урезан до RU+EN без потерь [П] | клон; VoiceDesign цепочкой | TTFA 60–140 мс (3060) [З] | [qwentts.cpp](https://github.com/ServeurpersoCom/qwentts.cpp) (CUDA) [?] | нет | — |
| **[Shtorm-VoxCPM2-RU](https://huggingface.co/ArtShtorm/Shtorm-VoxCPM2-RU)** (ArtShtorm) | 2026-10-06 | 2.3B (база VoxCPM2) [П]; VRAM [?] | Apache-2.0 | `+` перед гласной; односложные не размечать [П] | [?] | клон (5–15 с + транскрипт), voice design у базы | диффузионный AR; [?] | torch [?] | у базы VoxCPM есть сторонние [?] | — |
| [FireRedTTS3-Base-Russian](https://huggingface.co/shustov-te/FireRedTTS3-Base-Russian) | 2026-11-15 | FP32 [?] | Apache-2.0 | нет | [?] | zero-shot клон | стриминг через SGLang-Omni [З] | [?] | **`/v1/audio/speech` в sgl-omni [П]**, лимит 400 символов | — |
| [F5-TTS_RUSSIAN](https://huggingface.co/Misha24-10/F5-TTS_RUSSIAN) (Misha24-10), `v4_winter` | 2026-01-13 (после апреля ничего) | 335M, ≈3–5 ГБ | **CC-BY-NC-4.0** | `+` перед гласной [П] | в данных 240 ч Common Voice EN [П]; качество латиницы [?] | клон | не стриминг; ×2–4 RT | работает у владельца | нет | [демо v1](https://misha24-10.github.io/Misha24-10/) |
| [ESpeech-TTS-1 RL-V2](https://huggingface.co/ESpeech/ESpeech-TTS-1_RL-V2) | 2025-08-25 (без изменений) | F5-база, 5.4 ГБ на 3080¹ | Apache-2.0 | `+` (RUAccent) [П] | английский не поддерживается (voicy возвращает 400) [П] | клон до 12 с | ×2.5 RT¹ | как F5 | нет | лидерборд ниже |
| [f5-tts-russian-emotional](https://huggingface.co/Andreevai/f5-tts-russian-emotional) (Andreevai) | 2026-10-27 | 350M | MIT | `+` [П] | [?] | клон 3–10 с | как F5 | обучена **на RTX 5060** [З] | нет | Colab |
| [TVI/f5-tts-ru-accent](https://huggingface.co/TVI/f5-tts-ru-accent) | веса 2025-04; **ONNX-экспорт 2026-05-09** | 335M | [?] | `+` | [?] | клон | ONNX | — | нет | — |
| [VoXtream2-RU](https://huggingface.co/simba9/voxtream2-ru) (simba9) | 2026-08-27 | Mimi 12.5 Гц, малая [?] | OpenRAIL-M (только исследования) | [?] | [?] | клон 3–10 с | **full-stream, управление темпом** | [?] | нет | [samples](https://simba9-voxtream2-ru.static.hf.space/samples.html), [Habr](https://habr.com/ru/articles/1075298/) |
| [kokoro-ru](https://huggingface.co/zaakirio/kokoro-ru) (zaakirio) | 2026-07-29 / 08-25 | 82M, CPU ×9.8 RT | OpenRAIL (по метаданным) | через G2P: RUAccent → IPA; можно подать IPA [П] | G2P только русский, латиница = OOV [П] | 3 фиксированных голоса | очень быстро | CPU | нет | аудио на карточке |
| [Shtorm-PocketTTS-RU v2](https://huggingface.co/ArtShtorm/Shtorm-PocketTTS-RU) | 2026-09-25 / 10-15 | ~100M, CPU | CC-BY-4.0 | [?] | **латиница не поддерживается [П]** | клон | быстро, CPU | CPU | нет | — |
| [pocket-tts-ru](https://huggingface.co/amekhrishvili/pocket-tts-ru) | 2026-09-02 | ~100M | CC-BY-NC-4.0 | `+` обязателен (silero-stress) [П] | [?] | клон | CPU | CPU | нет | — |
| [Silero v5_5_ru](https://github.com/snakers4/silero-models) | v5.5 2026-02-03; эмфазы 2026-10 | ~140 МБ, CPU | CC-BY-NC-4.0 (v5_cis_base — MIT) | `+`, автоударения и омографы встроены [П] | [?] | 5 готовых голосов, без клона | до 100× RT на CPU [З] | CPU | нет | [Habr](https://habr.com/ru/articles/1094196/) |
| [AuK-ru](https://huggingface.co/aga7on/AuK-ru) (LoRA к Tencent AuK) | 2026-09-19 | 3B+ | MIT | [?] | **code-switching RU/EN не работает [П, по признанию автора]** | клон | — | — | — | не рекомендуется |

¹ замер voicy, `nvidia-smi` всей карты RTX 3080 вместе с ASR ([experiments/19](https://github.com/olluorg/voicy/tree/master/experiments/19-second-engine)).
² замер Igor282 на RTX 3060 Laptop. ³ см. `docs/tts-quality.md` этого репозитория: в июне 2026 1.7B «takes down GPU1 at the driver level».

**Чего не нашлось.** Русских fine-tune'ов Chatterbox, CosyVoice, Orpheus, XTTS, Fish/OpenAudio,
Higgs, MOSS, OmniVoice с открытыми весами и заметной аудиторией за период нет (поиск HF по именам
вида `*-ru`, `*-russian`). Есть только перезаливки: `Maks32rus/F5-TTS_RUSSIAN` и
`agiws/F5-TTS-RUSSIAN` (карточка байт-в-байт как у Misha24-10), `LeeAeron/F5TTSx-ESpeech-*`.
Большие корпорации открытых TTS-весов не выпускали: GigaTTS Сбера ([Habr, 2026-05-15](https://habr.com/ru/companies/sberbank/articles/966640/))
закрыта, у Т-Банка синтез только для юрлиц через API, MWS AI (Audiogram) и Yandex SpeechKit только облачные.
У Vikhr TTS-релизов в 2026 году нет (последнее — `Salt_new_early`, 2025-11).

---

## (б) Инструменты: ударения, нормализация, транслитерация

| Инструмент | Обновлён | Лицензия | Что делает | Точность / скорость | Встраивание |
|---|---|---|---|---|---|
| **[silero-stress](https://github.com/snakers4/silero-stress)** v1.5 | 2026-09-15 | MIT | ударения + ё + омографы, свои regex/фразы | ~4M словоформ 100%; 2.2K омографов F1 0.92, word-acc 94% [З, их README]; OOV-слова 60–70% [З]; на независимом HomographResolutionEval 82% ([issue #9](https://github.com/snakers4/silero-stress/issues/9)); 0.5 мс/слово на 1 потоке CPU | `pip install silero-stress`; выдаёт `+` перед гласной; флаги `put_stress_homo`, `put_yo_homo`, `words_to_ignore` |
| **[RUAccent](https://github.com/Den4ikAI/ruaccent)** 1.5.8.3 | 2026-07-17 (смена лицензии на MIT); веса `ruaccent/accentuator` 2026-07-17 | MIT | ударения, ёфикация, омографы | 95.2% на словах с заглавной буквы и 97.4% после приведения к нижнему регистру (замер siriusfreak на Russian LibriSpeech) [З]; 2–3% ошибок на обычном тексте [З] | `+` перед гласной → для Qwen-fine-tune конвертировать в U+0301 после гласной |
| [sh1man/ruaccent](https://github.com/sh1man/ruaccent) + [Sh1man/ruaccent-accentuator](https://huggingface.co/Sh1man/ruaccent-accentuator) | 2026-10-28 | MIT | форк RUAccent с резолвером «ё», дообученным на речи | восстановление «ё» 95.5% → **98.7%**, ложные «ё» 8.5% → 4.2% [З] | замена весов RUAccent (`repo=`) |
| [ruaccent-rs](https://github.com/alksef/ruaccent-rs) + [ruaccent-tiny-compact](https://huggingface.co/TTSBard/ruaccent-tiny-compact) | 2026-11-12 | [?] / MIT | порт RUAccent на Rust, INT8 + FST-словарь | 116 МБ RAM вместо 650, загрузка 0.35 с, совпадение 100% [З] | интересно для Go-сервиса: без Python |
| [omogre / udarenie](https://github.com/omogr/udarenie) | 2026-08-01 | [?] | ударения + IPA-транскрипция; udarenie добавляет морфологию Natasha и Wiktionary | [бенчмарк](https://github.com/omogr/russian-stress-benchmark) против silero-stress есть, но эталон не опубликован → чисел нет | IPA-выход годится для моделей на фонемах (XTTS-ru-ipa, kokoro-ru) |
| StressRNN / russtress | 2024 | Apache-2.0 | BiLSTM + словарь Зализняка | устарел, без омографов | не рекомендуется |
| **[ru-normalizr](https://github.com/NickZaitsev/ru-normalizr)** v1.3.0 | 2026-10-12 | MIT | числа, даты, время, сокращения, римские цифры, символы, URL; **латиница → кириллица** (`iPhone` → «айфоун») через IPA (`eng_to_ipa`) или словарь; можно с `+` в кириллизованных словах | точных метрик нет; заявлен «лучший no-LLM» [З] | `enable_latinization=True, latinization_backend="ipa", enable_latinization_stress_marks=True`; русские ударения не ставит, нужен отдельный акцентуатор |
| [runorm](https://github.com/Den4ikAI/runorm) | 2024-05 (не обновлялся) | Apache-2.0 | числа, английские слова, сокращения | — | запасной вариант |
| **[voicy: словарь терминов + autolex](https://github.com/olluorg/voicy/tree/master/experiments/15-autolex)** | 2026-07…11 | MIT | словарь из 79 IT-терминов (`API`→«апи», `JWT`→«джей-дабл-ю-ти», `JSON`→«джейсон»); аббревиатуры читаются по буквам **по правилу**; каждое произношение автоматически проверяется синтезом и обратным ASR | 78/79 терминов проходят авто-проверку [З] | [`rust/core/assets/pronunciation.json`](https://github.com/olluorg/voicy/blob/master/rust/core/assets/pronunciation.json); подход (правило для аббревиатур + проверка ASR) переносится в Go |
| meter2 (в репо siriusfreak) | 2026-10 | исследовательский (часть обучена на Silero NC) | **детектор ударения по аудио**: wav2vec2 + MLP | 98.3% на живой речи [З] | для собственной оценки моделей на тестовом тексте владельца |
| [ESpeech/GigaAM-v3-e2e-ctc-stressed](https://huggingface.co/ESpeech/GigaAM-v3-e2e-ctc-stressed) | 2026-10-20 | MIT | по названию — ASR, который выдаёт ударения [?] | карточка пустая | возможный инструмент оценки; не проверено |

**LLM-препроцессинг.** Отдельных опубликованных инструментов «LLM → текст для русского TTS» за
период не нашлось. Практика из найденных проектов: правила плюс словарь (voicy, ru-normalizr,
`latin_to_cyrillic` из [Habr 1107532](https://habr.com/ru/articles/1107532/)). Поскольку текст
и так генерирует кодинг-агент, самый дешёвый вариант — просить LLM в системном промпте сразу
выдавать «аудио-версию» с кириллическими терминами; voicy пришёл к тому же
([ADR 0002](https://github.com/olluorg/voicy/tree/master/docs/adr): «аудио-текст объясняет, а не транслитерирует»).

---

## (в) Подробности по значимым пунктам

**awesome-russian-speech.** С марта 2026 в репозитории два коммита: 2026-03-17 добавлен
`ru-normalizr` в «Общую предобработку», 2026-10-24 добавлен курс ИТМО. Раздел TTS не менялся
(F5: hotstone228, TVI, Misha24-10; Vosk-TTS, RHVoice, TeraTTS, XTTS-ru-ipa). Список больше не
отражает свежие модели: ни Qwen-ru-stress, ни VoxCPM2-RU, ни ESpeech там нет. [П]

**Qwen3-TTS 1.7B ru-stress-cf (siriusfreak).** LoRA r16 на talker поверх рецепта voicy, плюс
«контрфактный» штраф: тот же звук со сдвинутым знаком должен давать NLL как минимум на 5 nats
выше. Метки обучающих данных сверены с аудио детектором meter2. Результат на 130 фразах
(~730 знаков против словаря): 96% ±1.5 [П, карточка; оценка автоматическая, не слушатели].
На обычном тексте с разметкой RUAccent: 0.1% слов не по знаку, CER 2.9% (Whisper large-v3-turbo).
Вариант «IPA в фигурных скобках вместо знака» тоже дал 96%, но с худшим CER. Знак работает и
против словаря, поэтому ошибку RUAccent на жаргоне можно перебить своим словарём. В продакшне у
автора работает через vLLM-Omni v0.30.0 как обычная Base [З]. Есть MLX-кванты (3Xj90Ibl4U),
квант Q4 с урезанным словарём (Igor282), интеграция в [speech-studio](https://github.com/timoncool/speech-studio)
и упоминание в [Habr 1106246](https://habr.com/ru/articles/1106246/) (2026-11-10). Рекомендуют
и в [дискуссии Qwen3-TTS #185](https://github.com/QwenLM/Qwen3-TTS/discussions/185). ⚠ Данные:
тексты фраз со сдвинутыми ударениями взяты из `biggest-ru-book` без лицензии; meter2 — только для
исследований. ⚠ Наш риск: 1.7B ранее вешал GPU1 (`docs/tts-quality.md`). Варианты: запускать
на GPU0, через vLLM-Omni или через GGUF/qwentts.cpp.

**voicy (olluorg).** Единственный найденный проект, который системно исследует нашу задачу:
«как озвучивать инженерные тексты». Из него стоит взять три вывода. (1) [experiments/12](https://github.com/olluorg/voicy/tree/master/experiments/12-engine-survey):
на одном материале Qwen3-TTS 1.7B дал 38 пауз и 1.8% тишины против 123 пауз и 20% у ESpeech
(F5). «Рваность — свойство архитектуры F5/DiT, а не настроек» [З]. Цена: Qwen в 10 раз медленнее
(×0.4 против ×4.1 RT, без flash-attn). (2) [ADR 0015](https://github.com/olluorg/voicy/blob/master/docs/adr/0015-stress-control-in-qwen3-tts.md):
базовый Qwen3-TTS произносит `+` вслух («сто плюс ит»), от U+0301 коверкает слово, ЗАГЛАВНЫЕ
ударение не двигают. Отсюда понадобилась LoRA. (3) [experiments/14](https://github.com/olluorg/voicy/tree/master/experiments/14-lexicon):
латиница нестабильна («Generics» → «Генерикс», «type erasure» → «тип и резерв»), а кириллическое
«Интеджер» Whisper распознал как английское `integer`. Транслитерация даёт верное английское
чтение надёжнее латиницы. Политика словаря — кириллица. Сервер OpenAI-совместимый
(`/v1/audio/speech`, `pcm`), бинарник на Rust, Linux x86-64 + NVIDIA проверены [З].
Ударения ставит RUAccent, портированный в Rust.

**Shtorm-VoxCPM2-RU.** Слитая LoRA к VoxCPM2 (OpenBMB, 2.3B, Apache-2.0): ударение задаётся `+`
перед гласной. Обязательно `normalize=False`, иначе встроенный нормализатор портит метки.
Односложные слова не размечать, иначе получается эмфаза. Автор прямо пишет: «формальных замеров
нет, проверялась на слух» [П]. Сама VoxCPM2 на r/LocalLLaMA считается одной из лучших для
клонирования ([тред](https://www.reddit.com/r/LocalLLaMA/comments/1xcpjbk/)) [З]. Это главный
кандидат №2, если Qwen-вариант не устроит по тембру.

**FireRedTTS3-Base-Russian.** Слитая русская LoRA к FireRedTTS3, обслуживается через SGLang-Omni
с OpenAI-подобным `/v1/audio/speech` [П]. Разметки ударений нет. Опубликованы только дельты
метрик на 10 примерах [П]. Это низкий приоритет, но OpenAI-сервер «из коробки» удобен.

**F5-TTS русские.** См. раздел (д).

**ESpeech.** Новых TTS-весов после августа 2025 нет. Есть VAE (2025-12), денойзер (2026-06) и
`GigaAM-v3-e2e-ctc-stressed` (2026-10). Лидерборд [ESpeech/open_tts_leaderboard_ru](https://huggingface.co/spaces/ESpeech/open_tts_leaderboard_ru)
последний раз обновлялся 2026-06-03, свежий замер в нём от 2025-08-28. По нему ESpeech RL-V2
(CER 1.4, UTMOS 3.27) лучше F5 Misha V2 (CER 3.0, UTMOS 3.15). Новых моделей лидерборд не содержит. [П]

**Silero.** v5_5_ru (2026-02), SAPI5 (v5.6, 2026-06), эмфазы и восклицания (Habr, 2026-10-16).
Ударения и омографы встроены. Авторы анонсируют «большое обновление акустического звучания», но
оно не вышло [П]. Клонирования нет, по UTMOS старые версии были внизу лидерборда. Для нашей цели
это не основной синтезатор, но **silero-stress — сильный кандидат на роль акцентуатора**.

**Kokoro-ru, PocketTTS-RU, VoXtream2-RU** — маленькие быстрые модели. Kokoro-ru принимает IPA,
так что ударение полностью управляемо, но голоса фиксированные и латиницы нет. PocketTTS-RU
явно не читает латиницу. VoXtream2-RU — потоковая модель с CER ~1% [З]. Лицензия
исследовательская. По качеству до 1.7B-моделей они не дотягивают [З, по архитектуре].

**Сообщество.** На Habr за период о русском локальном TTS пишут Silero (stress v1.5, v5 эмфазы),
обзоры уровня «Silero/XTTS/F5 в одном окне» (там латиница переводится в кириллицу самописной
функцией) и статья про Qwen-ru-stress + Drama 3 для эмоций. На r/LocalLLaMA русский отдельно не
обсуждают. Общие фавориты осени 2026: MOSS-TTS 1.5, VoxCPM2, Higgs v3, Fish Drama 3, Chatterbox
Multilingual v3 (из них русский заявлен у Chatterbox и Drama 3). Это трек соседней сессии.
Telegram-каналы (`@speech_recognition_ru`, `@voicestuff`, `@TeraSpace`, `@decent_researcher`)
не просматривались.

---

## (г) Топ-5 моделей и связка препроцессинга

Критерий: русский + английские термины, максимум качества, помещается в 16 ГБ.

1. **Qwen3-TTS 1.7B-Base ru-stress-cf (siriusfreak)**: единственная модель с *измеренным*
   соблюдением ударения против словаря (96%). У неё база с лучшей просодией по замерам voicy,
   Apache-2.0 и клонирование. Риск — стабильность 1.7B на GPU1.
2. **Shtorm-VoxCPM2-RU**: сильная база с клонированием, ударение через `+`. Замеров нет, нужно
   слушать.
3. **F5-TTS_RUSSIAN v4_winter (Misha24-10)**: текущая установка, контрольная точка.
   NC-лицензия; рваность F5 подтверждена у voicy.
4. **ESpeech-TTS-1 RL-V2**: лучше F5-Misha по лидерборду, Apache-2.0, `+`; английский не
   поддерживается.
5. **FireRedTTS3-Base-Russian**: готовый OpenAI-совместимый сервер через SGLang-Omni, но без
   управления ударением и без замеров. Альтернатива на 5-е место — VoXtream2-RU, если
   нужен потоковый вывод.

**Рекомендуемая связка (для п. 1; для п. 2–4 то же, но с `+` вместо U+0301):**

```
текст агента
 → (опц.) LLM-промпт «аудио-версия»: без markdown/кода, числа словами
 → словарь IT-терминов (свой JSON, затравка — 79 терминов voicy):
     Postgres→«По́стгрес», Kibana→«Киба́на», race condition→«рейс конди́шн»,
     connection pool→«конне́кшн пул», хотфикс, деплой…  — сразу со знаком ударения
 → ru-normalizr (числа/даты/сокращения; enable_latinization + ipa-бэкенд
     как страховка для терминов вне словаря, с --with-latin-stress)
 → акцентуатор: silero-stress v1.5 или RUAccent (форк sh1man для «ё»);
     слова, уже размеченные словарём, пропускать (words_to_ignore / skip_regex);
     Title-case приводить к нижнему регистру перед RUAccent
 → конвертер: «+V» → «V́» (для Qwen), убрать знаки с односложных и с «ё»
 → TTS с language="Russian" и транскриптом референса
```

Главное: **в ней латиница не доходит до модели**. Это та же транслитерация, что и сейчас, только
автоматическая, со словарём и управляемым ударением. Ни одна найденная русская модель не
читает латиницу в русской фразе надёжно по измерениям.

---

## (д) F5-TTS русские fine-tune'ы: что изменилось с апреля 2026

| Чекпойнт | Последнее изменение | Что нового после апреля |
|---|---|---|
| Misha24-10/F5-TTS_RUSSIAN | **2026-01-13**: `F5TTS_v1_Base_v4_winter` (model_212000 + промежуточные) | ничего; карточка v4 не описывает |
| Misha24-10/test_model_f5tts_v3 | 2025-11-25 | ничего |
| ESpeech-TTS-1 (5 вариантов) | 2025-08-25 | ничего |
| TVI/f5-tts-ru-accent | веса 2025-04-19 | 2026-05-09 добавлена папка `onnx/` |
| hotstone228/F5-TTS-Russian | 2025-01-25 | ничего |
| Andreevai/f5-tts-russian-emotional | **2026-10-27, новый** | эмоциональный fine-tune (14 ч театральных диалогов), `+`, MIT |
| SWivid/F5-TTS (код) | 1.1.20 → 1.1.22 (2026-04-20…07-23) | мелкие релизы; архитектурно нового (F5 v2) нет |

**Стоит ли обновлять текущую установку?** Если стоит `v4_winter`, обновлять нечего. Если более
ранний (`v1_Base`, `accent_tune`, `v2`), можно взять `v4_winter`, но это то же семейство с теми
же ограничениями. **Вкладываться в F5 дальше не стоит.** Его главный недостаток (термины кириллицей)
снимается не новым чекпойнтом, а автоматическим препроцессингом из раздела (г), и этот
препроцессинг подходит к любой модели. «Рваность» F5 (паузы, 20% тишины против 1.8% у Qwen по
voicy) обновлением не лечится. Следующий шаг — A/B на тестовом тексте владельца: текущий F5
против Qwen-1.7B-ru-stress-cf против Shtorm-VoxCPM2-RU, с одним препроцессингом.

---

## (е) Что не удалось проверить

- **Качество на слух.** Ничего не синтезировалось. Все цифры ударений получены автоматическими
  детекторами авторов (meter2, контур громкости), а не слушателями.
- **Латиница в fine-tune'ах.** Как Qwen-ru-stress-cf (обучен только на русском), Shtorm-VoxCPM2-RU,
  FireRedTTS3-RU и F5 v4_winter читают латиницу в русской фразе, нигде не измерено. Не исключено,
  что русская LoRA ухудшила английский относительно базы.
- **sm_120 / Blackwell.** Конкретных отчётов «работает на RTX 50xx» для VoxCPM2-RU, FireRedTTS3,
  VoXtream2 и qwentts.cpp не нашлось. F5 и Qwen 0.6B у владельца уже работают. Andreevai обучал
  на RTX 5060 [З]. Причина зависания GPU1 на Qwen 1.7B не выяснена: модель это или драйвер/питание.
- **VRAM VoxCPM2-RU и FireRedTTS3-RU** — по картам не указаны.
- **Точность акцентуаторов на одном эталоне.** Бенчмарк omogre без опубликованного gold. У
  silero-stress и RUAccent цифры на разных наборах, напрямую их сравнить нельзя.
- **GigaAM-v3-e2e-ctc-stressed** — карточка пустая, назначение выведено из названия.
- **Telegram-каналы и чаты** (`@speech_recognition_ru`, `@voicestuff`, `@TeraSpace`) не читались.
  Брейв их не индексирует, браузер не использовался.
- **Fish Drama 3** заявляет phoneme-теги для произношения, но это мультиязычная модель на 35B MoE,
  трек соседней сессии.
- **Обзорный тред r/LocalTextToSpeech «My TTS list of 2026»** открыт не был. Это сводная таблица
  с демо-ссылками, полезна соседней сессии сравнений.
