# Manafield Manage

Manafield 인스턴스를 관측하고 관리하기 위한 **공개 모듈 모노레포**입니다.

**Manafield Core와는 별도 저장소입니다.** 각 모듈은 Module Protocol을 따르며 독립적으로 배포될 수 있습니다.

## Modules

| Directory | Module ID | Status | Purpose |
| --- | --- | --- | --- |
| `modules/manage-web/` | `manafield-manage-web` | Initial implementation | Core Registry의 Module / Resource / Capability 조회 |

추후 후보:
- `modules/manage-lifecycle/`: Module 재시작, 리빌드, 업데이트 요청 및 작업 상태 표시
- `modules/manage-monitor/`: Module 상태, 로그, 메트릭 관측
- `modules/manage-audit/`: 관리 작업 이력

후보 모듈은 **아직 구현되지 않았습니다**. 실제 운영 명령의 실행 권한은 웹 UI에 직접 부여하지 않고, Manafield의 별도 관리 API / 권한 제한된 실행 주체에 위임할 예정입니다.

## Development

```bash
cd modules/manage-web
go test ./...
go run ./cmd/manafield-manage-web
```

기본 웹 포트는 `8080`이며 `PORT` 또는 `MANAFIELD_MANAGE_ADDR`로 조정합니다. `MANAFIELD_CORE_URL`은 조회 대상 Core 주소입니다.

`manage-web`은 **읽기 전용 상태 화면**입니다. Docker 소켓 접근이나 Jenkins 관리 권한을 요구하지 않습니다.

## Deployment

모노레포 안의 모듈을 빌드할 때에는 `modules/manage-web`을 Docker build context로 사용합니다. Git 리비전과 모듈별 빌드 경로를 릴리스 메타데이터에 각각 남기는 방식을 지향합니다.

현재 서버에 배포된 기존 로컬 `manafield-manage` 모듈은 별도로 유지합니다. 공개 저장소로의 첫 코드 이전이 기존 인스턴스의 설정을 자동으로 변경하거나 배포하지는 않습니다.

## Related

- [Manafield Core / CLI](https://github.com/eventide-manafield/manafield)
- [Manafield Web Shell](https://github.com/eventide-manafield/manafield-web)

## License

라이선스 선정 전입니다. **공개 저장소라는 사실 자체가 사용·수정·재배포 허락을 의미하지는 않습니다.**
