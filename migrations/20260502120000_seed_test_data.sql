-- +goose Up
-- +goose StatementBegin

INSERT INTO users (id, email, slug, password_hash, full_name, role, created_at) VALUES
('11111111-1111-1111-1111-111111111111', 'admin@logiflow.ru',         'admin',            '$2a$10$5.Y78TsjpWNKfVWwcslumOLLhI1ooFsAFwkIHCEfE6mJbXEFcnPwK', 'Александр Петров',   'admin',   NOW() - INTERVAL '30 days'),
('22222222-2222-2222-2222-222222222222', 'manager.anna@logiflow.ru',  'manager-anna',     '$2a$10$s5kIprBFTItQRBwhor8MG.m2g/Ccb1FHlpUjAfecdf5RiADT7Fz3i', 'Анна Смирнова',      'manager', NOW() - INTERVAL '25 days'),
('33333333-3333-3333-3333-333333333333', 'manager.igor@logiflow.ru',  'manager-igor',     '$2a$10$T0BYHFrWFxAcCQ0ZFUDUS.lDaPbWCZ2ieqdmY6E6k9Lxgy52bvJEe', 'Игорь Козлов',       'manager', NOW() - INTERVAL '25 days'),
('44444444-4444-4444-4444-444444444444', 'driver.mikhail@logiflow.ru','driver-mikhail',   '$2a$10$/DQUJNlY5VPOrHDTWvemLenERJMaxQdjIrjr.k3vpiLAMbzOOvrOO', 'Михаил Соколов',     'driver',  NOW() - INTERVAL '20 days'),
('55555555-5555-5555-5555-555555555555', 'driver.dmitry@logiflow.ru', 'driver-dmitry',    '$2a$10$Lg/p0qsIGywuDbZhwYuKZuiGLjJJg2YPtZzk.z67XlkDu65amfAzu', 'Дмитрий Новиков',    'driver',  NOW() - INTERVAL '20 days'),
('66666666-6666-6666-6666-666666666666', 'driver.sergey@logiflow.ru', 'driver-sergey',    '$2a$10$bv3pyx1enKXe1ziDi7QnP.A/t9SShuVHaips1fWzS2VnrzhhokOo6', 'Сергей Волков',      'driver',  NOW() - INTERVAL '18 days'),
('77777777-7777-7777-7777-777777777777', 'kate@example.com',          'client-ekaterina', '$2a$10$AAZ8uznfK7QltkDyLYtX.ugucAPWx5VThoyGrz6UDoWdbgxT4lUZ2', 'Екатерина Морозова', 'client',  NOW() - INTERVAL '15 days'),
('88888888-8888-8888-8888-888888888888', 'alexey@example.com',        'client-alexey',    '$2a$10$UvO71Zhx1uMG1M9i68ZG9en3iVoGgvCWu/SK9B2D0sPyqg5GhLBvK', 'Алексей Попов',      'client',  NOW() - INTERVAL '10 days');

INSERT INTO vehicles (id, plate_number, brand, model, year, capacity_kg, capacity_m3, status, slug) VALUES
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'А123БВ77', 'ГАЗ',  'Газель Next', 2022,  1500.00,  10.00, 'available', 'gazelle-a123bv77'),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 'Е456КМ78', 'MAN',  'TGX 26.460',  2021, 18000.00,  90.00, 'in_use',    'man-tgx-e456km78'),
('cccccccc-cccc-cccc-cccc-cccccccccccc', 'О789НО54', 'Ford', 'Transit',     2023,  1000.00,   8.00, 'available', 'transit-o789no54');

INSERT INTO warehouses (id, slug, name, address, city, latitude, longitude, status, created_at) VALUES
('dddddddd-dddd-dddd-dddd-dddddddddddd', 'warehouse-moscow-1',      'Склад Москва №1',          'Ленинградский просп., 80',   'Москва',          55.802, 37.510, 'active', NOW() - INTERVAL '30 days'),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee', 'warehouse-spb-1',         'Склад Санкт-Петербург №1', 'Московский просп., 212',     'Санкт-Петербург', 59.849, 30.318, 'active', NOW() - INTERVAL '28 days'),
('ffffffff-ffff-ffff-ffff-ffffffffffff', 'warehouse-novosibirsk-1', 'Склад Новосибирск №1',     'ул. Бориса Богаткова, 256',  'Новосибирск',     54.981, 82.963, 'active', NOW() - INTERVAL '20 days');

INSERT INTO drivers (id, user_id, vehicle_id, license_number, license_expiry, rating, slug, status) VALUES
('a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1', '44444444-4444-4444-4444-444444444444', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'ВУ 123456', '2028-06-15', 4.80, 'driver-mikhail-sokolov', 'available'),
('b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2', '55555555-5555-5555-5555-555555555555', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 'ВУ 789012', '2027-11-30', 4.95, 'driver-dmitry-novikov',  'on_trip'),
('c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3', '66666666-6666-6666-6666-666666666666', 'cccccccc-cccc-cccc-cccc-cccccccccccc', 'ВУ 345678', '2029-03-20', 4.60, 'driver-sergey-volkov',   'available');

INSERT INTO managers (id, user_id, warehouse_id, slug) VALUES
('d4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4', '22222222-2222-2222-2222-222222222222', 'dddddddd-dddd-dddd-dddd-dddddddddddd', 'manager-anna-smirnova'),
('e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5', '33333333-3333-3333-3333-333333333333', 'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee', 'manager-igor-kozlov');

-- Цены рассчитаны по формуле: baseFee(500) + dist_km*25 + weight_kg*3 + volume_m3*150
-- pending:    Москва → Казань   843.8 km  500 kg  3 m3   → 500 + 21095 + 1500 + 450  = 23545.00
-- assigned:   Москва → Тула     195.3 km 1000 kg  5 m3   → 500 +  4882 + 3000 + 750  =  9132.50
-- in_transit: СПб   → Москва   700.3 km 5000 kg 30 m3   → 500 + 17507 + 15000 + 4500 = 37507.50
-- delivered:  Москва → Рязань   209.9 km  800 kg  4 m3   → 500 +  5247 + 2400 + 600  =  8747.50
-- cancelled:  Москва → Владимир 206.8 km  300 kg  2 m3   → 500 +  5170 +  900 + 300  =  6870.00
INSERT INTO orders (id, created_by_id, driver_id, manager_id, origin_warehouse_id, origin_address, destination_address, cargo_description, weight_kg, volume_m3, status, total_price, created_at, assigned_at, delivered_at) VALUES
('f6f6f6f6-f6f6-f6f6-f6f6-f6f6f6f6f6f6',
 '88888888-8888-8888-8888-888888888888', NULL,
 'd4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4', 'dddddddd-dddd-dddd-dddd-dddddddddddd',
 'Ленинградский просп., 80, Москва', 'ул. Баумана, 45, Казань',
 'Электроника и бытовая техника', 500.00, 3.00, 'pending', 23545.00,
 NOW() - INTERVAL '2 days', NULL, NULL),

('a7a7a7a7-a7a7-a7a7-a7a7-a7a7a7a7a7a7',
 '77777777-7777-7777-7777-777777777777', 'a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1',
 'd4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4', 'dddddddd-dddd-dddd-dddd-dddddddddddd',
 'Ленинградский просп., 80, Москва', 'просп. Ленина, 30, Тула',
 'Промышленное оборудование', 1000.00, 5.00, 'assigned', 9132.50,
 NOW() - INTERVAL '1 day', NOW() - INTERVAL '20 hours', NULL),

('b8b8b8b8-b8b8-b8b8-b8b8-b8b8b8b8b8b8',
 '88888888-8888-8888-8888-888888888888', 'b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2',
 'e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5', 'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee',
 'Московский просп., 212, Санкт-Петербург', 'ул. Тверская, 1, Москва',
 'Строительные материалы', 5000.00, 30.00, 'in_transit', 37507.50,
 NOW() - INTERVAL '3 days', NOW() - INTERVAL '2 days', NULL),

('c9c9c9c9-c9c9-c9c9-c9c9-c9c9c9c9c9c9',
 '77777777-7777-7777-7777-777777777777', 'a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1',
 'd4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4', 'dddddddd-dddd-dddd-dddd-dddddddddddd',
 'Ленинградский просп., 80, Москва', 'ул. Почтовая, 12, Рязань',
 'Продукты питания', 800.00, 4.00, 'delivered', 8747.50,
 NOW() - INTERVAL '5 days', NOW() - INTERVAL '4 days', NOW() - INTERVAL '3 days 20 hours'),

('d0d0d0d0-d0d0-d0d0-d0d0-d0d0d0d0d0d0',
 '88888888-8888-8888-8888-888888888888', NULL,
 'd4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4', 'dddddddd-dddd-dddd-dddd-dddddddddddd',
 'Ленинградский просп., 80, Москва', 'просп. Строителей, 7, Владимир',
 'Мебель', 300.00, 2.00, 'cancelled', 6870.00,
 NOW() - INTERVAL '3 days', NULL, NULL);

-- Маршруты для всех заказов. Координаты получены через OSRM (overview=simplified&geometries=geojson).
-- current_index для in_transit = 7 (36% пути, 3ч из 8.3ч)
-- current_index для delivered = 21 (финальная точка из 22, 0-based)
INSERT INTO routes (id, order_id, driver_id, coordinates, current_index, started_at, finished_at, distance_km, duration_sec, status) VALUES

-- pending: Москва → Казань, водитель ещё не назначен
('10101010-1010-1010-1010-101010101010',
 'f6f6f6f6-f6f6-f6f6-f6f6-f6f6f6f6f6f6', NULL,
 '[[37.510041,55.802052],[37.854128,55.708045],[38.111784,55.715512],[38.309945,55.818518],[39.177561,55.875421],[39.519295,55.990605],[40.164265,56.088596],[40.558232,56.011802],[40.851729,55.835349],[41.022957,55.81992],[41.347976,55.686557],[41.840806,55.621715],[41.99586,55.6434],[42.238563,55.575953],[43.942575,55.50577],[44.373133,55.438538],[44.685847,55.489047],[45.437828,55.449492],[45.585152,55.479144],[46.200278,55.435077],[46.295171,55.386601],[47.609698,55.324204],[48.649115,55.456509],[48.741014,55.617623],[48.892158,55.706553],[48.786678,55.739008],[48.855641,55.855923],[49.123115,55.789988]]',
 0, NULL, NULL, 843.80, 43474, 'pending'),

-- assigned: Москва → Тула, Михаил назначен, ещё не выехал
('20202020-2020-2020-2020-202020202020',
 'a7a7a7a7-a7a7-a7a7-a7a7-a7a7a7a7a7a7', 'a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1',
 '[[37.510041,55.802052],[37.564544,55.785467],[37.531328,55.752447],[37.550124,55.724772],[37.621355,55.705213],[37.596079,55.578673],[37.619262,55.459713],[37.60222,55.366227],[37.561285,55.325815],[37.527081,55.242087],[37.537188,55.175556],[37.584414,55.092389],[37.510818,54.882997],[37.461562,54.835208],[37.514665,54.718712],[37.496273,54.614617],[37.517654,54.534768],[37.495904,54.41023],[37.628518,54.227564],[37.617731,54.192422]]',
 0, NULL, NULL, 195.30, 9585, 'pending'),

-- in_transit: СПб → Москва, Дмитрий едет (3 часа из 8.3ч, точка 7 из 22)
('e1e1e1e1-e1e1-e1e1-e1e1-e1e1e1e1e1e1',
 'b8b8b8b8-b8b8-b8b8-b8b8-b8b8b8b8b8b8', 'b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2',
 '[[30.318011,59.848819],[30.368679,59.766774],[30.643043,59.653192],[30.677699,59.570117],[30.986831,59.436272],[31.23062,59.22487],[31.52155,58.743632],[32.709968,58.514254],[33.076182,58.339929],[33.356901,58.332933],[34.189673,57.715899],[34.313316,57.488582],[34.723128,57.34099],[35.108646,57.083493],[35.520423,56.941375],[36.086647,56.92268],[36.256642,56.708404],[36.447347,56.615868],[36.647876,56.312883],[36.878783,56.166606],[37.198427,56.090813],[37.616702,55.754544]]',
 7, NOW() - INTERVAL '3 hours', NULL, 700.30, 29866, 'active'),

-- delivered: Москва → Рязань, Михаил доставил (все 22 точки пройдены)
('f2f2f2f2-f2f2-f2f2-f2f2-f2f2f2f2f2f2',
 'c9c9c9c9-c9c9-c9c9-c9c9-c9c9c9c9c9c9', 'a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1',
 '[[37.510041,55.802052],[37.565642,55.784999],[37.651863,55.793295],[37.717239,55.75151],[37.740753,55.755902],[37.836847,55.713062],[37.828969,55.686252],[38.005744,55.604636],[38.030311,55.572554],[38.127575,55.517006],[38.249465,55.385297],[38.374901,55.363149],[38.378323,55.317968],[38.426603,55.254848],[38.623029,55.216955],[38.664944,55.195595],[38.690994,55.155755],[38.894317,55.104348],[38.900616,55.053778],[38.94574,55.009597],[39.562655,54.679065],[39.712018,54.627105]]',
 21, NOW() - INTERVAL '4 days', NOW() - INTERVAL '3 days 20 hours', 209.90, 11833, 'finished'),

-- cancelled: Москва → Владимир, маршрут не стартовал
('30303030-3030-3030-3030-303030303030',
 'd0d0d0d0-d0d0-d0d0-d0d0-d0d0d0d0d0d0', NULL,
 '[[37.510041,55.802052],[37.565642,55.784999],[37.651863,55.793295],[37.717239,55.75151],[37.740753,55.755902],[37.779003,55.729406],[37.854128,55.708045],[38.111784,55.715512],[38.221754,55.747165],[38.249947,55.795404],[38.309945,55.818518],[38.464198,55.82236],[38.533367,55.812861],[38.648412,55.83198],[38.752173,55.826382],[38.894957,55.838328],[39.125513,55.889299],[39.177561,55.875421],[39.266892,55.906228],[39.340261,55.914987],[39.383176,55.957481],[39.519295,55.990605],[39.849019,56.054845],[40.037738,56.053143],[40.150989,56.087572],[40.195341,56.078551],[40.407068,56.129017]]',
 0, NULL, NULL, 206.80, 11222, 'pending');

INSERT INTO notifications (id, user_id, title, body, is_read, created_at) VALUES
(gen_random_uuid(), '44444444-4444-4444-4444-444444444444', 'Новый заказ назначен',       'Вам назначен заказ Москва → Тула',                          false, NOW() - INTERVAL '20 hours'),
(gen_random_uuid(), '55555555-5555-5555-5555-555555555555', 'Маршрут начат',              'Маршрут СПб → Москва активен. Расстояние: 700 км',          true,  NOW() - INTERVAL '3 hours'),
(gen_random_uuid(), '77777777-7777-7777-7777-777777777777', 'Заказ доставлен',            'Ваш заказ (Рязань) успешно доставлен',                      true,  NOW() - INTERVAL '3 days 20 hours'),
(gen_random_uuid(), '77777777-7777-7777-7777-777777777777', 'Заказ назначен водителю',    'Ваш заказ назначен водителю Михаилу Соколову',              false, NOW() - INTERVAL '20 hours'),
(gen_random_uuid(), '88888888-8888-8888-8888-888888888888', 'Заказ отменён',              'Ваш заказ (Владимир) был отменён',                          true,  NOW() - INTERVAL '2 days 12 hours'),
(gen_random_uuid(), '88888888-8888-8888-8888-888888888888', 'Заказ принят в обработку',   'Ваш заказ (Казань) принят и ожидает назначения водителя',   false, NOW() - INTERVAL '2 days'),
(gen_random_uuid(), '22222222-2222-2222-2222-222222222222', 'Новый заказ на складе',      'Поступил новый заказ: склад Москва №1 → Казань',            false, NOW() - INTERVAL '2 days'),
(gen_random_uuid(), '33333333-3333-3333-3333-333333333333', 'Заказ в пути',               'Заказ СПб → Москва сейчас в пути (700 км)',                 true,  NOW() - INTERVAL '3 hours');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM notifications WHERE user_id IN (
    '11111111-1111-1111-1111-111111111111',
    '22222222-2222-2222-2222-222222222222',
    '33333333-3333-3333-3333-333333333333',
    '44444444-4444-4444-4444-444444444444',
    '55555555-5555-5555-5555-555555555555',
    '66666666-6666-6666-6666-666666666666',
    '77777777-7777-7777-7777-777777777777',
    '88888888-8888-8888-8888-888888888888'
);
DELETE FROM routes WHERE id IN (
    '10101010-1010-1010-1010-101010101010',
    '20202020-2020-2020-2020-202020202020',
    'e1e1e1e1-e1e1-e1e1-e1e1-e1e1e1e1e1e1',
    'f2f2f2f2-f2f2-f2f2-f2f2-f2f2f2f2f2f2',
    '30303030-3030-3030-3030-303030303030'
);
DELETE FROM orders WHERE id IN (
    'f6f6f6f6-f6f6-f6f6-f6f6-f6f6f6f6f6f6',
    'a7a7a7a7-a7a7-a7a7-a7a7-a7a7a7a7a7a7',
    'b8b8b8b8-b8b8-b8b8-b8b8-b8b8b8b8b8b8',
    'c9c9c9c9-c9c9-c9c9-c9c9-c9c9c9c9c9c9',
    'd0d0d0d0-d0d0-d0d0-d0d0-d0d0d0d0d0d0'
);
DELETE FROM managers WHERE id IN (
    'd4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4',
    'e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5'
);
DELETE FROM drivers WHERE id IN (
    'a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1',
    'b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2',
    'c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3'
);
DELETE FROM warehouses WHERE id IN (
    'dddddddd-dddd-dddd-dddd-dddddddddddd',
    'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee',
    'ffffffff-ffff-ffff-ffff-ffffffffffff'
);
DELETE FROM vehicles WHERE id IN (
    'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
    'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',
    'cccccccc-cccc-cccc-cccc-cccccccccccc'
);
DELETE FROM users WHERE id IN (
    '11111111-1111-1111-1111-111111111111',
    '22222222-2222-2222-2222-222222222222',
    '33333333-3333-3333-3333-333333333333',
    '44444444-4444-4444-4444-444444444444',
    '55555555-5555-5555-5555-555555555555',
    '66666666-6666-6666-6666-666666666666',
    '77777777-7777-7777-7777-777777777777',
    '88888888-8888-8888-8888-888888888888'
);

-- +goose StatementEnd
