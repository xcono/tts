# Тест OmniVoice (k2-fsa/OmniVoice, omnivoice 0.2.1) — 2026-11-17

Цель: быстро дать владельцу аудио на его тексте для сравнения на слух с Qwen3-TTS 1.7B ru-stress.
Сэмплы: `docs/research/samples/omnivoice/`. Лаборатория: `llm/lab/omnivoice/` (Dockerfile, synth.py,
обрезанные референсы `ref/`, тексты 04/07 `texts/`, `asr-transcripts.tsv`, лог `synth-step2.log`).

## Повторить

```bash
cd llm/lab/omnivoice && docker build -t xcono-tts-lab:omnivoice .   # FROM tf5151 + omnivoice, torch/transformers пинятся
cd ../../.. && docker run --rm --gpus device=0 -e HF_HUB_OFFLINE=1 -v llms_hf-cache:/root/.cache/huggingface \
  -v $PWD/llm/lab/omnivoice:/lab:ro -v $PWD/llm/lab/qwen3-ru-stress:/texts:ro -v $PWD/docs/research/samples/omnivoice:/out \
  xcono-tts-lab:omnivoice python3 /lab/synth.py --text-file /texts/text-A.txt --out /out/01-original-default
# варианты: --takes 1 --ref /lab/ref/ref_10s.wav --ref-text "..." --num-step 64 --guidance 3.0
```

Стек: torch 2.11.0+cu128, transformers 5.15.1, fp16 на `cuda:0`, `language="ru"`. Значения по умолчанию в пакете
`denoise=True`, `postprocess_output=True` — совпадают с рабочими настройками из статьи Habr/Raft.

## Файлы

Референс по умолчанию — `/app/ref_stas.wav` (11.8 с, полный транскрипт). Тексты: A = `qwen3-ru-stress/text-A.txt`
(оригинал, термины латиницей), D = `text-D.txt` (U+0301 на всех словах), C = `text-C.txt` (U+0301 на всех, термины
кириллицей). 04 = `omnivoice/texts/text-04-stress-jargon.txt`, 07 = `omnivoice/texts/text-07-cyr-nostress.txt`.

| Файл | Отличие | Текст | ASR (whisper :8008), заметные расхождения |
|---|---|---|---|
| 01-original-default-1/-2 | по умолчанию (32 шага, guidance 2.0), 2 дубля | A | «под рейсом» (трейсам), «Кибана»; в -1 «деплей»; в -2 «постгресс» |
| 09-ref5s | реф 3.8 с: «Это нелегко. Ты думаешь, меня окружают прекрасные люди.» | A | «ход фиксом», «постгресс», «под рейсом» |
| 02-ref10s | реф 5.75 с: до «…Лучшей командой желать нельзя.» | A | «500-ки», «под рейсом», «Кибана» |
| 03-steps64 | как 02, `num_step=64` | A | как 02 |
| 08-guidance3 | как 02, `guidance_scale=3.0` | A | «постгресс», «под рейсом» |
| 04-stress-jargon | U+0301 только: задепло́ил депло́й че́каут гре́плю зафи́ксил хо́тфиксом пятисо́тки | 04 | «под рейсом», «Кибана» |
| 05-stress-all | U+0301 на всех словах, термины латиницей | D | «чек-аут», «постгресс», «под рейсом» |
| 06-cyr-terms | U+0301 на всех словах, термины кириллицей | C | «чек-аут», «постгрессе», «под рейсом», «кибане» |
| 07-cyr-terms-nostress | термины кириллицей, без ударений | 07 | «по трейсам» верно; «пасгрессе», «деплей» |

Полные транскрипты — `llm/lab/omnivoice/asr-transcripts.tsv`. Обрезки текста в конце нет ни в одном файле
(последнее слово «эспрессо» распознано везде). «под рейсом» вместо «по трейсам» встречался и у Qwen3 — вероятно,
слабость ASR, а не синтеза. Ударения по ASR не оцениваются — их оценивает владелец на слух.

## Скорость и VRAM (RTX 5060 Ti 16 ГБ, GPU 0)

- Загрузка модели 1.1 с (из кэша). Синтез ~23–24 с аудио за 3.2–4.3 с (RTF 0.13–0.18); `num_step=64` — 6.3 с (RTF 0.27).
- Пик `torch.cuda.max_memory_allocated` 2.3 ГиБ. После остановки GPU 0 вернулась к 1.63 ГБ.
- Длительность определяется текстом и референсом (NAR-модель): 02/03/08 и 06/07 совпадают до 0.03 с.

## Что пошло не по плану

- Загрузка весов через Xet зависла на 3.09 из 3.26 ГБ (сеть и CPU в нуле). Недостающий `audio_tokenizer`
  дотянут по HTTP с `HF_HUB_DISABLE_XET=1` примерно за минуту. Общее время до первого аудио ~13 мин.
- `02-ref10s` по указанию оркестратора обрезан по «…нельзя.», поэтому длина 5.75 с, а не ~10 с; вариант до «…не так.»
  (~8.4 с) не синтезировался.
- Транскрипт референса в задании — «пройти не так», Whisper слышит «пойти не так»; в полных прогонах оставлен текст задания.
