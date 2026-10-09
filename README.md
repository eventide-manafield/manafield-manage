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

### 공개 UI와 인스턴스별 개인 테마

공개 저장소의 기본 웹 UI는 흰 배경, 진한 글자, 회색 경계선만 사용하는 **문서형·중립 디자인**입니다. 개인이 사용하는 색상, 애니메이션, 브랜드 스킨은 공개 저장소에 넣지 않습니다.

로컬로 복제한 뒤 `modules/manage-web/custom.css` 파일만 추가하세요. 이 파일은 `.gitignore`로 제외되며 Go 바이너리에 포함되지 않습니다.

```bash
cd modules/manage-web
cat > custom.css <<'CSS'
:root {
  --page-bg: #fafafa;
  --page-text: #222222;
  --page-muted: #555555;
  --page-border: #cccccc;
  --page-subtle: #f5f5f5;
}
CSS

go run ./cmd/manafield-manage-web
```

서버를 켠 상태에서 `custom.css`를 수정하고 브라우저를 새로고침하면 적용됩니다. Go 리빌드나 서버 재시작은 필요하지 않습니다. 기본 스타일은 `web/static/app.css`이며, `custom.css`가 **그 뒤에 로드**되므로 CSS 변수나 선택자를 자유롭게 재정의할 수 있습니다.

다른 위치의 파일을 사용하려면 `MANAFIELD_MANAGE_CUSTOM_CSS_FILE`을 지정합니다.

```bash
MANAFIELD_MANAGE_CUSTOM_CSS_FILE=/home/me/themes/manage.css go run ./cmd/manafield-manage-web
```

Docker에서는 외부 CSS 파일을 **읽기 전용 볼륨**으로 마운트하고 동일한 환경 변수를 설정하세요. 웹 앱은 `/static/custom.css`를 통해 지정된 파일만 읽어 제공합니다. **개인 테마를 담은 파일은 별도 비공개 위치에 보관**하는 게 좋습니다.

> 주의: 개인 CSS를 서버에 설정하면 그 CSS는 해당 웹페이지를 방문한 브라우저에도 전달됩니다. Git 저장소에 넣지 않는 것과 웹 방문자에게 CSS를 숨기는 것은 다릅니다. 이 공개 저장소의 이전 커밋에 포함됐던 디자인 또한 Git 기록에 남아 있습니다.

### Web UI 클래스와 기능 ID 규칙

공개 Web UI는 중립적인 흑백 문서형 스타일을 유지하며, 모든 화면 전용 CSS 클래스는 `mf-` 접두사를 사용합니다. 컴포넌트 단위로 `mf-panel`, `mf-panel__header`, `mf-list-item`, `mf-list-item__name`처럼 역할을 구분하고, 상태와 변형에는 `mf-status--ok`, `mf-button--danger`처럼 `--` 접미사를 사용합니다.

- **클래스(`class`)**: 재사용 가능한 시각적 구성·상태·커스터마이징 대상.
- **ID(`id`)**: 화면에서 하나만 존재하는 기능적 고정 영역. 예: `#module-panel`, `#resource-panel`, `#core-connection-status`.
- **데이터 속성**: 개수가 변하는 Module 및 Resource는 ID를 임의 생성하지 않고 `data-module-id` / `data-resource-id`로 구분.
- **작업 버튼**: 일반 작업은 `mf-button`, 서비스 재시작·리빌드·업데이트 등 서버 상태를 바꾸는 작업은 `mf-button mf-button--danger`을 사용. 버튼에 고유한 실행 기능을 구현할 때만 안정적인 `id`를 부여합니다.

현재 공개 `manage-web`은 읽기 전용이어서 서버 변경 버튼을 실제 화면에 표시하지 않습니다. 배포 엔진 및 권한 검증이 구현되기 전까지 관리 작업을 수행할 수 없습니다. 예시 화면의 버튼은 기능을 연결하지 않은 미리보기 요소입니다.

### 모듈 필수 바인딩 및 상세 진단

Manage Web의 모듈 목록은 Core Registry에 등록된 `requires.capabilities`를 읽고 `필수 바인딩 : n/m`을 표시합니다. 여기서 `m`은 **필수 슬롯 수**, `n`은 **대상 Instance ID가 실제 지정된 필수 슬롯 수**입니다. `n/m`은 유효성 판정이 아니므로 바인딩은 `2/2`여도 Provider 부재·Capability 불일치·계약 버전 불일치 경고가 표시될 수 있습니다.

각 모듈의 **상세보기**는 `/modules/{id}`에서 전체 요구사항 수(필수/선택), 슬롯 별 요구 Capability, 버전 범위, 바인딩 대상, 현재 Registry에 등록된 Provider 정보와 진단 결과를 표시합니다. 버전 검사는 SemVer 범위 문법을 사용하며, 검사할 수 없는 버전은 정상이라고 추정하지 않습니다. Core v0의 Requirement는 모두 필수이며, 향후 명시적 `optional` 값이 지원되는 경우 선택 슬롯도 분류합니다.

현재 Core `/modules`에는 **요구사항은 있지만 실제 Binding 대상 데이터는 없습니다.** Manage가 바인딩을 추정하지 않도록, 선택적 읽기 전용 스냅샷을 따로 공급할 수 있습니다.

```json
{
  "modules": {
    "example-module": {
      "state": "main-postgres"
    }
  }
}
```

이 스냅샷은 **현재 활성 인스턴스에서 확정된 바인딩 정보**만 기록해야 합니다. `instance.yaml` 전체를 읽게 하지 말고, `modules[].bindings`에서 Module ID와 슬롯→대상 ID만 추출해서 생성하세요. 비밀번호, 환경변수, Resource Config, Token은 절대로 넣지 않습니다.

예를 들어 `/run/manafield/manage/bindings.json`에 읽기 전용 마운트를 설정하고 다음 환경변수를 추가합니다.

```text
MANAFIELD_MANAGE_BINDINGS_FILE=/run/manafield/manage/bindings.json
```

설정하지 않거나 파일을 읽지 못하면 **필수 바인딩 : 확인 불가**로 표시하고 `0/m`으로 추정하지 않습니다. 요구사항이 없는 모듈만 `0/0`으로 표시합니다. 스냅샷이 설정된 경우 목록에 없는 모듈의 Binding은 지정되지 않은 것으로 취급하므로, 배포 시 모듈 목록 전체를 포함한 최신 스냅샷으로 원자적으로 교체해야 합니다.

현재 버전에서는 스냅샷 추출/동기화 자동화와 Core Binding 조회 API는 아직 제공하지 않습니다. Manage Web 자체는 읽기 전용이며, 배포·권한 변경은 수행하지 않습니다.

## Deployment

모노레포 안의 모듈을 빌드할 때에는 `modules/manage-web`을 Docker build context로 사용합니다. Git 리비전과 모듈별 빌드 경로를 릴리스 메타데이터에 각각 남기는 방식을 지향합니다.

현재 서버에 배포된 기존 로컬 `manafield-manage` 모듈은 별도로 유지합니다. 공개 저장소로의 첫 코드 이전이 기존 인스턴스의 설정을 자동으로 변경하거나 배포하지는 않습니다.

## Related

- [Manafield Core / CLI](https://github.com/eventide-manafield/manafield)
- [Manafield Web Shell](https://github.com/eventide-manafield/manafield-web)

## License

라이선스 선정 전입니다. **공개 저장소라는 사실 자체가 사용·수정·재배포 허락을 의미하지는 않습니다.**
