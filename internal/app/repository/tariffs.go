package repository

func seedTariffs() []CloudTariff {
	return []CloudTariff{
		{
			TariffID:         1,
			TariffName:       "AWS EC2 medium",
			ShortDescription: "Новый тариф для небольших проектов от AWS",
			PricePerMonth:    400,
			RAMGB:            4,
			ImageKey:         "aws_ec2_medium.jpg",
			VideoKey:         "aws_ec2_medium.mp4",
			TariffStatus:     StatusPublished,
			LikedByUserIDs:   makeUserIDs(228),
		},
		{
			TariffID:         2,
			TariffName:       "Yandex 1C Pro",
			ShortDescription: "Виртуальная машина для постоянно работающего сайта с резервом оперативной памяти.",
			PricePerMonth:    600,
			RAMGB:            8,
			ImageKey:         "yandex_vm_pro.jpg",
			VideoKey:         "yandex_vm_pro.mp4",
			TariffStatus:     StatusPublished,
			LikedByUserIDs:   makeUserIDs(115),
		},
		{
			TariffID:         3,
			TariffName:       "Azure AI Medium",
			ShortDescription: "Конфигурация для ресурсоёмкого API, аналитики и прикладных задач машинного обучения.",
			PricePerMonth:    12000,
			RAMGB:            32,
			ImageKey:         "azure_ai_medium.jpg",
			VideoKey:         "azure_ai_medium.mp4",
			TariffStatus:     StatusPublished,
			LikedByUserIDs:   makeUserIDs(456),
		},
		{
			TariffID:         4,
			TariffName:       "Google AI Max",
			ShortDescription: "Высокопроизводительный облачный экземпляр для интенсивных вычислений и больших нагрузок.",
			PricePerMonth:    40000,
			RAMGB:            128,
			ImageKey:         "google_ai_max.jpg",
			VideoKey:         "google_ai_max.mp4",
			TariffStatus:     StatusPublished,
			LikedByUserIDs:   makeUserIDs(789),
		},
		{
			TariffID:         5,
			TariffName:       "Start",
			ShortDescription: "Идеальное предложение для небольших проектов",
			PricePerMonth:    200,
			RAMGB:            2,
			ImageKey:         "Start.jpg",
			VideoKey:         "Start.mp4",
			TariffStatus:     StatusDraft,
			LikedByUserIDs:   []int{},
		},
		{
			TariffID:         6,
			TariffName:       "Legacy Hosting",
			ShortDescription: "Архивный тариф, исключённый из публикации.",
			PricePerMonth:    500,
			RAMGB:            1,
			ImageKey:         "legacy_hosting.jpg",
			VideoKey:         "legacy_hosting.mp4",
			TariffStatus:     StatusDeleted,
			LikedByUserIDs:   []int{3},
		},
		{
			TariffID:         7,
			TariffName:       "Yandex Storage Lite",
			ShortDescription: "Свое хранилище для малых проектов",
			PricePerMonth:    1500,
			RAMGB:            16,
			ImageKey:         "Y_store.jpg",
			VideoKey:         "Y_store.mp4",
			TariffStatus:     StatusPublished,
			LikedByUserIDs:   makeUserIDs(354),
		},
		{
			TariffID:         8,
			TariffName:       "AWS GS Medium",
			ShortDescription: "Оптимальное решение для собственного игрового сервера.",
			PricePerMonth:    5000,
			RAMGB:            128,
			ImageKey:         "aws_game.jpg",
			VideoKey:         "aws_game.mp4",
			TariffStatus:     StatusPublished,
			LikedByUserIDs:   makeUserIDs(479),
		},
	}
}

func makeUserIDs(count int) []int {
	ids := make([]int, count)
	for index := range ids {
		ids[index] = index + 1
	}
	return ids
}
