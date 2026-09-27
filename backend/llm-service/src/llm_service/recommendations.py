"""Deterministic improvement recommendations for the Grade service."""

RECOMMENDATIONS = {}


def _register(codes, text):
    for code in codes.split():
        RECOMMENDATIONS[code] = text


_register('DOC_PURPOSE_MISSING DOC_PURPOSE_UNCLEAR',
          'Уточните, какую задачу решает проект, для чего его используют и какой результат он даёт.')
_register('DOC_CAPABILITIES_MISSING DOC_CAPABILITIES_INCOMPLETE',
          'Перечислите основные возможности проекта и кратко опишите их назначение.')
_register('DOC_LOCAL_RUN_MISSING DOC_LOCAL_RUN_COMMANDS_INCOMPLETE',
          'Опишите последовательность команд от подготовки окружения до первого запуска.')
_register('DOC_LOCAL_RUN_PREREQUISITES_INCOMPLETE',
          'Укажите необходимые инструменты, их версии и внешние сервисы для запуска.')
_register('DOC_LOCAL_RUN_DEPENDENCIES_INCOMPLETE',
          'Добавьте команды установки зависимостей и укажите, из какой папки их выполнять.')
_register('DOC_LOCAL_RUN_CONFIGURATION_INCOMPLETE',
          'Опишите обязательные настройки и приведите пример конфигурации без реальных секретов.')
_register('DOC_LOCAL_RUN_VERIFICATION_INCOMPLETE',
          'Добавьте ожидаемый вывод, адрес страницы или проверочную команду для подтверждения успешного запуска.')
_register('DOC_BUILD_INSTRUCTIONS_MISSING DOC_BUILD_INSTRUCTIONS_INCOMPLETE',
          'Укажите команды сборки, порядок их выполнения и рабочую папку.')
_register('DOC_BUILD_CONFIGURATION_INCOMPLETE',
          'Опишите необходимые параметры, профили и переменные окружения для сборки.')
_register('DOC_BUILD_OUTPUT_UNCLEAR',
          'Укажите название и расположение результата сборки.')
_register('DOC_TESTING_INSTRUCTIONS_MISSING DOC_TESTING_COMMANDS_INCOMPLETE',
          'Добавьте команды запуска тестов и условия их выполнения.')
_register('DOC_TESTING_ENVIRONMENT_INCOMPLETE',
          'Опишите подготовку тестовых данных, настроек и необходимых сервисов.')
_register('DOC_TESTING_SCOPE_UNCLEAR',
          'Поясните, какие проверки выполняет каждая тестовая команда.')
_register('DOC_USAGE_EXAMPLE_MISSING DOC_USAGE_EXAMPLE_INCOMPLETE',
          'Добавьте воспроизводимый пример основного сценария с входными данными и ожидаемым результатом.')
_register('DOC_USAGE_INPUTS_UNCLEAR',
          'Опишите обязательные параметры, форматы входных данных и приведите пример.')
_register('DOC_USAGE_RESULT_UNCLEAR',
          'Покажите ожидаемый ответ, вывод команды или создаваемый файл.')
_register('DOC_STRUCTURE_OVERVIEW_MISSING DOC_STRUCTURE_OVERVIEW_INCOMPLETE',
          'Кратко опишите назначение основных каталогов и компонентов проекта.')
_register('DOC_NAVIGATION_INCOMPLETE',
          'Добавьте ссылки на основные инструкции и разделы документации.')
_register('DOC_REFERENCE_NOT_FOUND DOC_LINK_BROKEN',
          'Проверьте указанный путь или якорь и обновите ссылку на существующий объект.')
_register('DOC_COMMAND_INCONSISTENT DOC_SCRIPT_NOT_FOUND',
          'Сверьте команду с доступными скриптами проекта и исправьте её в инструкции.')
_register('DOC_CONFIGURATION_INCONSISTENT',
          'Согласуйте названия и значения настроек в документации с конфигурацией проекта.')
_register('DOC_COMPONENT_DESCRIPTION_INCONSISTENT',
          'Обновите описание компонента в соответствии с его текущим назначением.')
for name, text in {
    'README': 'Добавьте в README описание проекта и ссылки на основные инструкции.',
    'LICENSE': 'Укажите выбранные владельцем проекта условия использования в LICENSE.',
    'CONTRIBUTING': 'Опишите в CONTRIBUTING порядок подготовки и отправки изменений.',
    'CODEOWNERS': 'Укажите в CODEOWNERS ответственных за основные части проекта.',
}.items():
    for suffix in ('MISSING', 'EMPTY'):
        RECOMMENDATIONS[f'DOC_{name}_{suffix}'] = text

FALLBACKS = {
    'required_files': 'Дополните соответствующий файл документации актуальной информацией.',
    'purpose': 'Уточните назначение проекта и его основные возможности.',
    'local_run': 'Дополните инструкцию запуска воспроизводимыми шагами и проверкой результата.',
    'build': 'Уточните команды, настройки и результат сборки.',
    'testing': 'Уточните команды тестирования, подготовку окружения и состав проверок.',
    'usage': 'Дополните пример использования входными данными и ожидаемым результатом.',
    'structure': 'Уточните назначение компонентов и навигацию по документации.',
    'consistency': 'Сверьте описанные команды, ссылки и настройки с текущими файлами проекта.',
}


def render_advice(fact):
    """Return only an improvement recommendation for the Grade service."""
    return RECOMMENDATIONS.get(fact['code'], FALLBACKS[fact['subcategory']])
