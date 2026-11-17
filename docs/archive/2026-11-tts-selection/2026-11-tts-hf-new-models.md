# Новые/обновлённые открытые TTS-модели с русским (апрель–ноябрь 2026)

Трек: мультиязычные и «большие» модели на Hugging Face и GitHub.
Дата сбора: 2026-11-17. Железо-цель: RTX 5060 Ti 16 ГБ (sm_120), одна модель на одну карту.
Задача: русская речь с английскими IT-терминами (code-switching), максимум качества.

**Метод.** HF API `pipeline_tag=text-to-speech`: trending (200 позиций), `sort=createdAt` (200),
`sort=lastModified` (200), сортировка по лайкам (200). Фильтр `language=ru` в API **не работает**
(игнорируется), поэтому русский определялся по тегу `ru` в карточке и по тексту README.
Прочитаны README ~30 карточек (`huggingface.co/<id>/raw/main/README.md`), README репозиториев
через `gh`, Brave-поиск (Reddit r/LocalLLaMA, обзоры, GitHub issues). Ничего не устанавливалось,
веса не скачивались (только JSON API и размеры файлов из `api/models/<id>/tree`).

Обозначения: **[карточка]** — подтверждено карточкой модели/README/кодом; **[заявл.]** —
утверждение вендора без независимой проверки; **[отзыв]** — сторонний отзыв; **[?]** — не проверено.

## Главное в трёх строках

1. **Ни одна новая мультиязычная модель не даёт штатного управления русским ударением.**
   ZONOS3-Mini принимает IPA-группы, но знаки ударения в них **отбрасываются** [карточка, docs/guide.md];
   MOSS-TTS даёт IPA/пиньинь (примеры только zh/en); OmniVoice — пиньинь и CMUdict (только zh/en).
2. **Единственный найденный прямой ответ на «ударения + качество Qwen3»** — русские стресс-дообучения
   Qwen3-TTS-1.7B, которые слушаются U+0301 (`siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf`, окт. 2026:
   96% следования знаку против словаря). Формально это трек «русская экосистема», но для шорт-листа он ключевой.
3. Из новых крупных мультиязычных моделей по качеству русского на бумаге лидируют **Higgs TTS 3 (4B)**,
   **Fish Drama 3 (35B MoE)**, **VoxCPM2 (2B)**, **MOSS-TTS v1.5**; **ZONOS3-Mini** (владелец назвал явно) —
   быстрая и Apache-2.0, но по MOS качество звука ниже Fish S2 Pro и Qwen3-TTS [отзыв MarkTechPost].

## (а) Сводная таблица кандидатов (русский заявлен, вышли/обновились апр–ноя 2026)

| Модель | Релиз / посл. обновл. | Размер, VRAM | Лицензия | RU официально | Ударение / произношение | Клон / голоса | Стрим, скорость | Сервер `/v1/audio/speech` | sm_120 / рантайм |
|---|---|---|---|---|---|---|---|---|---|
| [Zyphra/ZONOS3-Mini](https://huggingface.co/Zyphra/ZONOS3-Mini) | 2026-11-09 / 11-16 | 0.8–0.9B MoE (100M акт.), LM 1.8 ГБ + кодек; GGUF ~0.7 ГБ | Apache-2.0 | да, 1 из 43 (объём RU не раскрыт) | IPA-группы на слово, **ударения в IPA выбрасываются**; лексикон закрыт | да (клип или эмбеддинг), 27 голосов | да, TTFA 80 мс H100 [заявл.] | да (SGLang-Omni и свой CUDA-сервер) | CUDA-сервер, ggml/CUDA, WebGPU; sm_120 [?] |
| [fishaudio/drama-3](https://huggingface.co/fishaudio/drama-3) | 2026-10-01 / 10-29 | 35B MoE (3B акт.), FP8 ≈36 ГБ; GGUF Q4_K_M 20.6 ГБ, Q2_K 15.9 ГБ | Fish Audio Research/Community (некоммерч.) | да, в списке 83 языков (тиры не указаны) | нет; теги эмоций/событий | да; спикер-теги 0–15 | да, TTFA 89 мс H200 [заявл.] | SGLang-/vLLM-Omni (`/v1/tts`), llama.cpp `/tts` | **не влезает целиком в 16 ГБ**; audio.cpp с выгрузкой экспертов в ОЗУ; на 5060 Ti запускают [отзыв] |
| [bosonai/higgs-tts-3-4b](https://huggingface.co/bosonai/higgs-tts-3-4b) | 2026-06-04 / 09-04 | ~4.7B, bf16 9.3 ГБ | Boson Research & Non-Commercial (+Creator Use Grant) | да, тир «WER <5» (85 языков) | нет фонемного ввода; инлайн-теги скорости/пауз/питча | да, zero-shot | да, SSE-стрим | да (SGLang-Omni, vLLM-Omni) | SGLang-Omni ориентирован на датацентровые GPU (issue #1120) [?] |
| [openbmb/VoxCPM2](https://huggingface.co/openbmb/VoxCPM2) | 2026-04-03 / 11-09 | 2.3B, ~8 ГБ VRAM [карточка] | Apache-2.0 | да, 1 из 30, 2M+ ч всего | нет; ударения угадывает и **не управляется** [отзыв] | да + voice design текстом | да, RTF 0.3 (4090) / 0.13 nano-vLLM | да (vLLM-Omni) | torch≥2.5, CUDA≥12 — ставить cu128 вручную |
| [OpenMOSS-Team/MOSS-TTS-v1.5](https://huggingface.co/OpenMOSS-Team/MOSS-TTS-v1.5) | 2026-05-25 / 05-26 | 8.5B, bf16 ≈17 ГБ — **на грани/не влезает** | Apache-2.0 | да, 1 из 31, рекомендуют `language` тег | пиньинь / IPA (примеры zh/en), контроль длительности | да | long-form, стрим через SGLang-Omni | SGLang-Omni [?] | **pin torch 2.9.1+cu128** — Blackwell-совместимо |
| [MOSS-TTS-Local-Transformer-v1.5](https://huggingface.co/OpenMOSS-Team/MOSS-TTS-Local-Transformer-v1.5) | 2026-06-04 / 06-18 | 4.55B, bf16 ≈9 ГБ | Apache-2.0 | да, 31 язык | как v1.5 | да | да | SGLang-Omni [?] | torch 2.9.1+cu128 |
| [Edge0/Audio8-TTS-Realistic-Air](https://huggingface.co/Edge0/Audio8-TTS-Realistic-Air) | 2026-10-14 / 11-16 | 0.6B + кодек; F32-бандл 3.8 ГБ | Apache-2.0 | да (добавлен в этой версии); RU WER 7.26 CV3 [карточка] | нет | да (нужен транскрипт референса) | да (llama.cpp) | да (адаптер SGLang-Omni) | torch≥2.5, тест 2.8 на H20 |
| [netease-youdao/Confucius4-TTS](https://huggingface.co/netease-youdao/Confucius4-TTS) | 2026-06-02 / 11-04 | размер не указан [?] | Apache-2.0 (код) | да, 1 из 14; RU WER 4.64 (MiniMax-MLS) | нет | да, кросс-язычный перенос голоса | [?] | [?] | требует CUDA 12.6 — для sm_120 пересобрать на cu128 [?] |
| [k2-fsa/OmniVoice](https://huggingface.co/k2-fsa/OmniVoice) | 2026-03-30 / 07-03 (код 11-16) | 0.6B | код Apache-2.0, **веса CC-BY-NC** | да, 600+ языков | пиньинь (zh), CMUdict (en) — для RU нет | да + voice design | RTF 0.025 [заявл.] | [?] | torch 2.8.0+cu128 в README |
| [Supertone/supertonic-3](https://huggingface.co/Supertone/supertonic-3) | 2026-05-06 / 05-18 | ~99M, ONNX, CPU | OpenRAIL-M | да, 1 из 31 | нет | фикс. голоса | очень быстрый | нет | ONNX; **репозиторий архивирован в сент. 2026** [отзыв yaps.ai] |
| [KittenML/kitten-tts-2-mini](https://huggingface.co/KittenML/kitten-tts-2-mini) | 2026-11-13 | 82M, ONNX int8 83 МБ | Apache-2.0 | да, 1 из 16 | нет | 118 голосов, англ. голоса говорят по-русски | CPU real-time | нет | ONNX/CPU |
| [FashionFlora/ogg-tts](https://huggingface.co/FashionFlora/ogg-tts) | 2026-10-30 / 11-12 | 0.75B | other [?] | да, ~645 ч RU из 8.2k ч | нет | да | блочный стрим | нет | обучена на RTX PRO 6000 Blackwell [карточка] |
| [kugelaudio/kugelaudio-0-open](https://huggingface.co/kugelaudio/kugelaudio-0-open) | 2026-01-11 / 09-23 | 7B (VibeVoice), ~19 ГБ | MIT | в списке, но «качество сильно разнится» | нет | да | [?] | [?] | **не влезает в 16 ГБ** |
| [ScenemaAI/scenema-audio](https://huggingface.co/ScenemaAI/scenema-audio) | 2026-04-26 / 05-26 | диффузия из LTX-2.3, int8 4.9 ГБ + Gemma, всего ~38 ГБ загрузки | other | да, 1 из 13 | нет | да | RTF ~0.66 на 4090 | Docker API | 16 ГБ — только с Gemma на CPU, 32 ГБ ОЗУ |
| [FINAL-Bench/Darwin-TTS-1.7B-Cross](https://huggingface.co/FINAL-Bench/Darwin-TTS-1.7B-Cross) | 2026-04-15 / 07-23 | 2.1B | Apache-2.0 | наследует 10 языков Qwen3-TTS | нет | да | как Qwen3-TTS | нет | как Qwen3-TTS |
| [ResembleAI/chatterbox](https://huggingface.co/ResembleAI/chatterbox) (Multilingual V3) | карточка обновлена 2026-06-10 | 0.5B | MIT | да, 23 языка | нет | да | [?] | нет (сторонние) | [?] |
| Qwen3-TTS (база, янв. 2026) + **RU-стресс-дообучения** ([siriusfreak](https://huggingface.co/siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf), [Igor282 GGUF](https://huggingface.co/Igor282/qwen3-tts-12hz-1.7b-ru-pruned-enru-Q4_K_M)) | 2026-10-04 / 2026-11-16 | 1.9B bf16 (~4–6 ГБ); Q4_K_M 1.1 ГБ | Apache-2.0 | да, 1 из 10 у Qwen3 | **U+0301 после гласной; 96% против словаря** [карточка, свой детектор] | клон (Base) | да; RTF ~0.2 на 3060 Laptop (GGUF) [карточка] | через vLLM-Omni / qwentts.cpp | qwen-tts на torch cu128 |

Новых **официальных** версий Qwen3-TTS (Qwen3.5-TTS и т.п.) в открытых весах не найдено: Qwen-Audio-3.0-TTS (июль 2026) —
только API [отзыв note.com]. CosyVoice 3.5 — только API; открытым остаётся Fun-CosyVoice3-0.5B-2512 (дек. 2025).
Fish S2.1 Pro (июль 2026) — **закрытые веса**, только API.

## (б) Кандидаты подробно

**ZONOS3-Mini (Zyphra).** Вышла 9–10 ноября 2026, Apache-2.0, 43 языка, русский в списке [карточка]. Архитектура:
латентный MoE 800–886M параметров (≈100M активных), кодек ZAC-1 15 Гц → 44.1 кГц, текст — SuperBPE; NeMo-нормализация
текста только для 9 языков, **русский идёт «как написано»** (числа/сокращения надо нормализовать самим) [карточка].
Есть пофразовые/пословные контроли (громкость, полоса, темп), акцентные адаптеры, редактирование речи. Управление
произношением: слово можно записать IPA-группой; в оценке Zyphra имя в IPA произносится как написано в 95% случаев,
но **«Stress marks, syllable dots and spaces are dropped as in training»** — то есть ударение напрямую не задаётся,
только через качество гласных (редукция в русском частично кодирует ударение — непроверенная гипотеза); «золотой»
лексикон лежит в приватном репо, открыто доступен только espeak-ng (GPL, opt-in) [GitHub docs/guide.md].
Рантаймы: собственный CUDA-сервер, SGLang-Omni (OpenAI `/v1/audio/speech`, стрим PCM), ggml (CUDA/Vulkan), MLX,
WebGPU в браузере — можно послушать без установки в [Zonos Voice Studio](https://zyphra.com/zonos3-mini/localplayground#cast);
есть сторонние GGUF (`ggml-org/ZONOS3-Mini-GGUF`, `AtomicChat/...`). Качество: все бенчмарки собственные (ZTTS2-Eval);
macro-WER 6.71% по 55 наборам; по MSR-UTMOS 3.07 против 3.16 у Fish S2 Pro и 3.54 у Qwen3-TTS-1.7B, клонирование
по клипу заметно хуже, чем по эмбеддингу (индекс 44.7 против 66.2) [отзыв MarkTechPost, softtechhub]. На Reddit:
«сэмплы удивительно хороши для размера, но явно не лучше Fish и ElevenLabs» [отзыв]. Отзывов про русский не найдено.
Старших ZONOS3 (не-Mini) в открытом доступе нет; предыдущая [ZONOS2](https://huggingface.co/Zyphra/ZONOS2)
(июнь 2026, ~8B MoE, `model.pth` 15.3 ГБ, русский — Tier 2, Linux-сервер на Mini-SGLang) в 16 ГБ впритык.

**Fish Audio Drama 3.** 1 октября 2026, переупакована 29 октября (новый чекпойнт). 35B MoE / 3B активных, гибридное
внимание, мультиспикерные теги, редактирование через `previous_takes`, 83 языка, русский в списке [карточка]; лицензия
некоммерческая. Официально — SGLang-Omni/vLLM-Omni, на H200 TTFA ~89 мс, RTF 0.13 [заявл.]. Официальный рантайм
требует 48 ГБ [отзыв Reddit], но есть GGUF: `ggml-org/drama-3-GGUF` (Q4_K_M 20.6 ГБ, **клонирование в llama.cpp
«пока не поддерживается»**), `Jmica/Drama-3-GGUF` для audio.cpp с выгрузкой экспертов на CPU (Q2_K 15.9 ГБ),
ComfyUI-ноды «от 8 ГБ VRAM» с int8 [отзыв r/comfyui], `fishaudio/drama-3-int4`. На 5060 Ti пользователи пишут
«18–21 с на 5–10 с речи, но звучит отлично» [отзыв Reddit]. Для владельца: две карты по 16 ГБ теоретически
позволяют Q4_K_M split, но это нарушает условие «одна модель в 16 ГБ»; на одной карте — только с CPU-оффлоадом и медленно.
Про русский конкретно отзывов нет. Предшественник S2 Pro (март 2026, 4.4B, русский — Tier 2) на русском
**не слушается знаков ударения** [отзыв chillyweather/russian-tts].

**Higgs TTS 3 (Boson AI, она же Higgs Audio v3 TTS).** 4 июня 2026, обновлена 4 сентября. ~4.7B, 24 кГц, 8 кодбуков
25 fps, `model.safetensors` 9.3 ГБ — влезает в 16 ГБ в bf16 [карточка]. Русский — в тире «WER/CER < 5,
production-quality» из 85 языков [карточка]. Инлайн-теги эмоций, стиля, скорости, пауз, высоты; фонемного ввода нет.
Сервер — SGLang-Omni / vLLM-Omni с OpenAI-совместимым `/v1/audio/speech` и SSE-стримом. Лицензия: исследовательская
некоммерческая + Creator Use Grant (личное использование ок). Отзывы: «у higgs audio v3 одно из лучших
мультиязычных произношений из одноязычного референса» [отзыв Reddit]; в собственной Emergent-оценке Boson в категории
«сложное произношение» win-rate лишь 25.1% [заявл., aiadoptionagency]; бенчмарк Emergent TTS включает «foreign words» —
косвенно code-switching. Есть ComfyUI/transformers-реализация `multimodalart/higgs-audio-v3-tts-4b-transformers`
и сторонний пайплайн «батчинг/стрим на 3090» [отзыв]. Русских отзывов не найдено.

**VoxCPM2 (OpenBMB).** 3 апреля 2026, обновлена 9 ноября. 2B (MiniCPM-4), без токенизатора (диффузионный AR),
48 кГц, 30 языков без языкового тега, 2M+ ч данных, ~8 ГБ VRAM, RTF ~0.3 на 4090 [карточка]. Apache-2.0.
Voice design текстом, «ultimate cloning» с транскриптом, LoRA-дообучение от 5–10 мин аудио — **реальный путь
дообучить на русском со знаками ударения**. Независимый тест (chillyweather/russian-tts, Mac): VoxCPM2, Qwen3-TTS
и Fish S2 Pro «угадывают ударение», и знаки ударения не помогают [отзыв; для VoxCPM2 знаки явно не тестировались].
На Reddit её ставят в одну тройку с OmniVoice и S2 [отзыв]. RU WER 4.53 в таблице Confucius4 (MiniMax-MLS).

**MOSS-TTS v1.5 и Local-Transformer v1.5 (OpenMOSS).** 25 мая / 4 июня 2026, Apache-2.0, 31 язык с русским [карточка].
Основная v1.5 — 8.5B (bf16 ~17 ГБ, в одну 16-ГБ карту без квантизации не влезает); Local-Transformer — 4.55B (~9 ГБ).
Ввод пиньиня/IPA и контроль длительности на токен, паузы `[pause 3.2s]`, long-form до часа [карточка, GitHub].
IPA-пример в README — только английский, и неясно, учитываются ли знаки ударения [?]. **pyproject пинит
torch 2.9.1+cu128** — самый «Blackwell-готовый» рантайм из всех. На Reddit (июнь 2026) MOSS 1.5 8B — «лучший
клон на английском», есть и противоположные мнения [отзыв]. Русских отзывов нет.

**Audio8-TTS-Realistic-Air (Edge0).** 14 октября 2026, 0.6B (DualAR в духе Fish S2), кодек 44.1 кГц, Apache-2.0;
русский добавлен в этой версии [карточка]. Собственная оценка: RU WER 7.26 / SIM 0.645 на CV3 — заметно хуже лидеров
[карточка]. Есть llama.cpp-интеграция (16 ноября), SGLang-Omni-адаптер с `/v1/audio/speech`, Space на ZeroGPU.
Малая и свежая — кандидат «попробовать», но не лидер.

**Confucius4-TTS (NetEase Youdao).** Июнь 2026, обновлена 4 ноября, Apache-2.0, 14 языков с русским, упор на
кросс-язычный перенос голоса без акцента [карточка]. В их таблице MiniMax-MLS RU WER: Confucius4 4.64, Qwen3-TTS 3.21,
Fish S2 4.24, OmniVoice 4.63, VoxCPM2 4.53, ElevenLabs 3.88 — полезная сводка русского WER (но от авторов модели).
Требует CUDA 12.6; размер модели в карточке не указан [?]. Space: `CharlesW/Confucius4-TTS`.

**OmniVoice (k2-fsa).** Март 2026 (до окна), код обновлялся 16 ноября 2026; 0.6B, 600+ языков, **веса CC-BY-NC**.
Произношение — только пиньинь и CMUdict, для русского нет [GitHub]. Популярна на Reddit как «daily driver» [отзыв].
Есть GGUF (`Serveurperso/OmniVoice-GGUF`), Space `k2-fsa/OmniVoice`. README: torch 2.8.0+cu128.

**Qwen3-TTS + русские стресс-дообучения.** Базовая Qwen3-TTS (янв. 2026) уже известна владельцу. Новое в окне:
[siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf](https://huggingface.co/siriusfreak/qwen3-tts-12hz-1.7b-ru-stress-cf)
(4 окт. 2026) — LoRA r=16 на talker, слияние в drop-in чекпойнт для `qwen_tts`/vLLM-Omni; ударение знаком **U+0301
после гласной**; следует знаку против словаря в 96% (база 17%, предыдущий voicy LoRA 77%), CER 2.9% через Whisper;
рекомендуют RUAccent для авторазметки [карточка; оценка собственным детектором meter2, выложен в репо].
[Igor282/...-enru-Q4_K_M](https://huggingface.co/Igor282/qwen3-tts-12hz-1.7b-ru-pruned-enru-Q4_K_M) (16 ноября) —
то же стресс-дообучение, словарь урезан под RU+EN (lossless для обоих), Q4_K_M 1.1 ГБ, RTF ~0.2 на 3060 Laptop,
рантайм qwentts.cpp. Поведение английских вставок после дообучения не измерялось [?]. Дубликат с треком
«русская экосистема» — оставляю здесь, потому что это прямое решение главной проблемы Qwen3.

**Мелкие мультиязычные с русским** (качество ниже цели, полезны как быстрый fallback): Supertonic 3 (99M, ONNX,
31 язык, лидер Open TTS Leaderboard по англ. WER; репозиторий архивирован), Kitten-TTS 2 Mini (82M, 16 языков,
англ. голоса говорят по-русски), MOSS-TTS-Nano-100M (апр. 2026, 20 языков), ogg-tts (0.75B, 645 ч русского
студийной записи — для 0.75B немало).

## (в) Топ-5 для «русский + английские термины, максимум качества»

1. **Qwen3-TTS-1.7B + `siriusfreak/...-ru-stress-cf`** — единственный кандидат, где решена главная боль (ударения):
   U+0301 + RUAccent, качество базовой Qwen3 (лучший RU WER 3.21 в таблице MiniMax-MLS и лучший MOS среди
   сравнивавшихся с ZONOS3), английский остаётся родным языком модели → вставки должны читаться по-английски,
   Apache-2.0, 4–6 ГБ VRAM, OpenAI-сервер через vLLM-Omni. Риск: не проверено, не испортило ли дообучение английские вставки.
2. **Higgs TTS 3 (4B)** — русский в «production»-тире, 9.3 ГБ bf16, отзывы о сильном мультиязычном произношении,
   стрим и `/v1/audio/speech`. Нет управления ударением; некоммерческая лицензия (для личного использования ок);
   риск — SGLang-Omni на sm_120.
3. **Fish Drama 3** — вероятно, максимальное качество/выразительность из открытых, но 35B MoE: в одну 16-ГБ карту
   не помещается без CPU-оффлоада (или split на две карты). Ударения не управляются (у S2 Pro знаки игнорировались).
   Брать, если владелец готов ослабить условие «одна модель на одну карту» ради качества.
4. **VoxCPM2 (2B)** — Apache-2.0, 48 кГц, 8 ГБ, без языкового тега (удобно для смешанного текста), есть LoRA-дообучение —
   запасной путь «дообучить на русском с ударениями», если ни одна модель не справится из коробки.
5. **ZONOS3-Mini** — назван владельцем; стоит прослушать (можно прямо в браузере), Apache-2.0, очень быстрый,
   IPA-ввод для отдельных слов (хорошо для английских терминов, если модель коверкает их). Но звук по MOS ниже
   Qwen3/Fish, русский не нормализуется, ударения в IPA выбрасываются.

Альтернативы на 5-е место: MOSS-TTS-Local-Transformer-v1.5 (лучшая Blackwell-готовность, IPA-ввод) и Confucius4-TTS.

## (г) Отсеянные

- Breeze-TTS-2 (BreezeBlue) — только en/zh (хотя на Reddit «лучшая открытая»).
- IndexTTS-2.5 (авг. 2026) — zh/en/ja/es/ar, русского нет; лицензия bilibili.
- Voxtral-4B-TTS-2603 — 9 языков без русского; CC-BY-NC.
- Fish S2.1 Pro, Qwen-Audio-3.0-TTS, CosyVoice 3.5 — закрытые веса (только API).
- Fun-CosyVoice3-0.5B-2512 — русский есть, но релиз дек. 2025, обновлений весов в окне нет (код — май 2026).
- Fish S2 Pro — до окна (март 2026), знаки ударения игнорирует [отзыв]; оставлен как эталон для сравнения.
- kugelaudio-0-open — 7B, ~19 ГБ VRAM, русский «сниженного качества» по словам авторов.
- MOSS-TTS v1.5 (8B основная) — ~17 ГБ bf16, без квантизации не влезает (Local-Transformer — влезает).
- Scenema Audio — на 16 ГБ только с Gemma на CPU и 32 ГБ ОЗУ, ~38 ГБ весов; нишевая.
- Darwin-TTS-1.7B-Cross — слияние весов Qwen3 без обучения; улучшение не подтверждено независимо.
- Ming-3.0-tts, phonon-40m-multilingual, Kyutai pocket-tts, NeuTTS-2e, Magpie-multilingual, TADA-3b-ml,
  Raon-OpenTTS, Inflect, mithi, maya2, rumik-oss-1, VieNeu, Irodori, Underdog — нет русского.
- VibeVoice (en/zh), Kokoro-82M (нет ru), Orpheus, Sesame CSM, Dia/Dia2, Spark-TTS, MaskGCT, Parler, OuteTTS — без
  русского или без обновлений в окне.
- Chatterbox Multilingual V3 — русский есть (23 языка), обновление 2026, но 0.5B и без контроля ударений; уровень
  ниже топ-5 [?, не слушал].
- Dramabox, chatterbox-flash/turbo — только английский.
- Phoonnx/vits-* (сотни конвертаций 16.11.2026) — старые VITS/Piper-голоса, не новые модели.

## (д) Что не удалось проверить

- **Качество русского и code-switching на слух** — ни одна модель не прослушана; отзывов именно о русском
  с английскими вставками для новых моделей (ZONOS3, Drama 3, Higgs 3, VoxCPM2, MOSS v1.5) не найдено.
- Как стресс-дообучения Qwen3 читают английские слова после дообучения.
- Работает ли SGLang-Omni (нужен для Higgs/Drama/ZONOS3/MOSS) на sm_120; явных issue про RTX 50xx не нашёл,
  есть только roadmap «consumer GPU support» (sglang-omni#1120). Известная ловушка Blackwell — PyTorch-колёса без
  sm_120 (fish-speech#1126 на RTX 5090): везде ставить torch ≥2.7 с индекса cu128.
- Учитывает ли MOSS-TTS знаки ударения в IPA для русского.
- Результаты Open TTS Leaderboard (HF, есть русский CV3) — данные в приватном датасете, Space не открывал в браузере.
- Размер и VRAM Confucius4-TTS; языки tencent/AuK и dots.tts (карточки их не перечисляют).
- Объём русских данных почти у всех мультиязычных моделей не раскрыт (исключение — ogg-tts: ~645 ч).

## Источники (ключевые)

- HF API: `https://huggingface.co/api/models?pipeline_tag=text-to-speech&sort={trendingScore,createdAt,lastModified,likes}`
- Карточки моделей по ссылкам в таблице; ZONOS3 guide: https://github.com/Zyphra/ZONOS3-Mini (docs/guide.md, «Pronunciation input»)
- ZONOS3 отзывы: https://www.marktechpost.com/2026/11/10/zyphra-zonos3-mini-open-source-tts/ ,
  https://www.reddit.com/r/LocalLLaMA/comments/1xtb9gw/zyphrazonos3mini_hugging_face/
- Drama 3 на 16 ГБ: https://huggingface.co/Jmica/Drama-3-GGUF , https://huggingface.co/ggml-org/drama-3-GGUF ,
  https://www.reddit.com/r/LocalLLaMA/comments/1x8kces/ , https://www.reddit.com/r/comfyui/comments/1x5ynn4/
- Ударения у Qwen3/Fish/VoxCPM2: https://github.com/chillyweather/russian-tts/issues/3
- Reddit-сравнения: https://www.reddit.com/r/LocalLLaMA/comments/1x5qr7b/ , https://www.reddit.com/r/LocalLLaMA/comments/1xcpjbk/
- Open TTS Leaderboard: https://huggingface.co/blog/open-tts-leaderboard
- Fish S2.1 Pro закрыта: https://lukeocodes.dev/fish-audio-52m-seed-s21-pro
- Blackwell/sm_120 ловушка: https://smeltcore.com/recipes/openaudio-s1-mini-on-rtx-5060-13-language-distilled-tts-in-5-gb-vram/
