# REST Appendix Listings

Подготовленные листинги содержат только актуальный код REST-репозитория.
Файлы удобно открывать в VS Code и копировать в приложение к ВКР с сохранением форматирования.

## Основные листинги
- `A1_process_call_handler.go` — HTTP-обработчик полного сценария `process-call`
- `A2_transcription_server.py` — REST-сервис транскрибации
- `A3_nlp_preprocess.py` — NLP-предобработка текста обращения
- `A4_router_classification.py` — классификация и маршрутизация обращения
- `A5_entity_extractor.py` — извлечение сущностей
- `A6_ticket_creator.go` — формирование и создание тикета
- `A7_simpleone_adapter.go` — интеграция с тикет-системой
- `A8_process_review_handler.go` — обработка ручной валидации маршрутизации

## Экспериментальные и сервисные листинги
- `B1_preprocess_text.py` — утилита пакетной предобработки текстов
- `B2_finetuned_training_dataset.py` — подготовка обучающей выборки для router
- `B3_finetuned_training_loop.py` — обучение и оценка fine-tuned router
- `B4_import_router_finetuned_model.py` — импорт fine-tuned артефактов router
- `B5_evaluate_routing_csv.py` — оценка маршрутизации по CSV
- `B6_evaluate_transcription_wer.py` — оценка качества транскрибации
- `B7_evaluate_labeled_audio_dataset.py` — end-to-end оценка на размеченном наборе аудио
