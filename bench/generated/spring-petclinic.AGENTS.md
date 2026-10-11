# AGENTS.md

Spring Boot Petclinic (Java 17, Thymeleaf, Spring Data JPA). Both Maven (`pom.xml`, primary; used in CI) and Gradle (`build.gradle`) builds exist.

## Commands

```bash
./mvnw spring-boot:run            # run on http://localhost:8080 (H2 in-memory by default)
./mvnw -B verify                  # full build + tests (what CI runs)
./mvnw test                       # tests only
./mvnw test -Dtest=OwnerControllerTests            # single test class
./mvnw test -Dtest=OwnerControllerTests#processCreationFormSuccess   # single method
./mvnw spring-javaformat:apply    # auto-format code (format is enforced in `validate` phase)
./mvnw package -P css             # recompile SCSS -> src/main/resources/static/resources/css (Maven only)
./gradlew bootRun | build | test --tests '*OwnerControllerTests'   # Gradle equivalents
```

- Lint: there is no separate lint command. `spring-javaformat:validate` and the nohttp Checkstyle check (`src/checkstyle/`) are bound to Maven's `validate` phase, so any `./mvnw test`/`verify` fails on bad formatting or `http://` URLs. Fix formatting with `spring-javaformat:apply`.
- Gradle: `formatMain`/`formatTest` tasks depend on the Checkstyle tasks.
- `MySqlIntegrationTests` uses Testcontainers (needs Docker); `PostgresIntegrationTests` uses Docker Compose (`docker-compose.yml`, services named `mysql`/`postgres`).

## Architecture

Package-by-feature under `org.springframework.samples.petclinic`: `owner` (Owner, Pet, Visit, PetType + controllers/repositories), `vet`, `model` (shared base entities `BaseEntity`/`NamedEntity`/`Person`), `system` (config, welcome/error pages).

Things that span several files:

- **Database selection**: default `application.properties` sets `database=h2`, and `spring.sql.init.schema-locations`/`data-locations` resolve to `db/${database}/schema.sql` and `data.sql`. The `mysql` / `postgres` profiles (`application-mysql.properties`, `application-postgres.properties`) override `database` and the datasource (env vars `MYSQL_URL/USER/PASS`, `POSTGRES_URL/USER/PASS`). Schema is managed by these SQL scripts (`ddl-auto=none`), so entity changes need matching edits in all three `db/*/schema.sql` files.
- **Caching**: `VetRepository` methods use `@Cacheable("vets")`; the cache is created in `system/CacheConfiguration` (JCache).
- **i18n**: UI strings come from `src/main/resources/messages/messages_*.properties`. `I18nPropertiesSyncTest` fails on hard-coded text in HTML templates and on keys missing from any language file, so new strings must be added to every `messages_*.properties`.
- **Native image/AOT**: `PetClinicRuntimeHints` registers resources (`db/*`, `messages/*`) and serialization hints for entities; many web tests are annotated `@DisabledInNativeImage`/`@DisabledInAotMode`.
- **Dev entry points in tests**: `PetClinicIntegrationTests` (H2 + devtools), `MysqlTestApplication`, and `PostgresIntegrationTests` have `main()` methods to run the app in an IDE with the matching DB.
