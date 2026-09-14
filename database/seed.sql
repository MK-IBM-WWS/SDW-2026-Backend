-- Выполните этот файл через пункт «SQL-запрос» в Adminer после миграции.
-- Скрипт можно запускать повторно: ON CONFLICT не создаёт дубликаты.

INSERT INTO users (user_id, login, password_hash, is_moderator, created_at)
VALUES
    (1, 'student', 'lab2-password-not-for-production', false, NOW()),
    (2, 'moderator', 'lab2-password-not-for-production', true, NOW()),
    (3, 'guest', 'lab2-password-not-for-production', false, NOW())
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO cloud_tariffs (
    tariff_id,
    tariff_name,
    short_description,
    tariff_status,
    image_url,
    video_url,
    price_per_month,
    ram_gb,
    created_at,
    creator_id,
    formed_at
)
VALUES
    (
        1,
        'AWS EC2 Medium',
        'Новый тариф для небольших веб-проектов и учебных серверов.',
        'опубликован',
        'http://localhost:9000/cloud-tariffs/aws_ec2_medium.jpg',
        'http://localhost:9000/cloud-tariffs/aws_ec2_medium.mp4',
        400,
        4,
        NOW() - INTERVAL '10 days',
        1,
        NOW() - INTERVAL '9 days'
    ),
    (
        2,
        'Yandex Cloud Pro',
        'Виртуальная машина для постоянно работающего сайта с резервом оперативной памяти.',
        'опубликован',
        'http://localhost:9000/cloud-tariffs/yandex_vm_pro.jpg',
        'http://localhost:9000/cloud-tariffs/yandex_vm_pro.mp4',
        600,
        8,
        NOW() - INTERVAL '8 days',
        2,
        NOW() - INTERVAL '7 days'
    ),
    (
        3,
        'Azure AI Medium',
        'Конфигурация для ресурсоёмкого API, аналитики и прикладных задач машинного обучения.',
        'опубликован',
        'http://localhost:9000/cloud-tariffs/azure_ai_medium.jpg',
        'http://localhost:9000/cloud-tariffs/azure_ai_medium.mp4',
        12000,
        32,
        NOW() - INTERVAL '6 days',
        1,
        NOW() - INTERVAL '5 days'
    ),
    (
        4,
        'Google AI Max',
        'Высокопроизводительный облачный экземпляр для интенсивных вычислений и больших нагрузок.',
        'опубликован',
        'http://localhost:9000/cloud-tariffs/google_ai_max.jpg',
        'http://localhost:9000/cloud-tariffs/google_ai_max.mp4',
        40000,
        128,
        NOW() - INTERVAL '4 days',
        2,
        NOW() - INTERVAL '3 days'
    ),
    (
        5,
        'Selectel Start',
        '',
        'черновик',
        'http://localhost:9000/cloud-tariffs/start.jpg',
        'http://localhost:9000/cloud-tariffs/start.mp4',
        0,
        0,
        NOW() - INTERVAL '2 days',
        2,
        NULL
    ),
    (
        6,
        'Legacy Hosting',
        'Архивный тариф, исключённый из публикации.',
        'удален',
        'http://localhost:9000/cloud-tariffs/legacy_hosting.jpg',
        'http://localhost:9000/cloud-tariffs/legacy_hosting.mp4',
        500,
        1,
        NOW() - INTERVAL '20 days',
        1,
        NOW() - INTERVAL '19 days'
    )
ON CONFLICT (tariff_id) DO NOTHING;

INSERT INTO user_tariff_likes (like_id, user_id, tariff_id, created_at)
VALUES
    (1, 1, 2, NOW()),
    (2, 2, 1, NOW()),
    (3, 3, 1, NOW()),
    (4, 3, 3, NOW()),
    (5, 1, 4, NOW())
ON CONFLICT (like_id) DO NOTHING;

-- Синхронизация последовательностей после явного задания ID.
SELECT setval(pg_get_serial_sequence('users', 'user_id'), COALESCE(MAX(user_id), 1), true) FROM users;
SELECT setval(pg_get_serial_sequence('cloud_tariffs', 'tariff_id'), COALESCE(MAX(tariff_id), 1), true) FROM cloud_tariffs;
SELECT setval(pg_get_serial_sequence('user_tariff_likes', 'like_id'), COALESCE(MAX(like_id), 1), true) FROM user_tariff_likes;
