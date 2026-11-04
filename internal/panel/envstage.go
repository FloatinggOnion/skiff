package panel

import (
	"time"
)

// envStageTable reserves a pending app name to the team staging its env, so another team can't read, overwrite or inherit those vars. The owner is kept in the database so a restart doesn't forget it; once a reservation is old, another team may take the name, but only after the staged vars are cleared.
type envStageTable struct{}

const envStageTTL = 30 * time.Minute

var envStage = &envStageTable{}

func (e *envStageTable) owner(app string) (team string, until time.Time, ok bool) {
	var u int64
	if sqlDB.QueryRow(`SELECT team, until FROM env_stage WHERE app=?`, app).Scan(&team, &u) != nil {
		return "", time.Time{}, false
	}
	return team, time.Unix(u, 0), true
}

func (e *envStageTable) reserve(app, team string, now time.Time) bool {
	cur, until, ok := e.owner(app)
	if ok && cur != team {
		if now.Before(until) {
			return false
		}
		_ = setEnv(app, nil)
	}
	if !ok {
		// Nothing says who staged vars already stored under a free name, so they're not this team's to read.
		_ = setEnv(app, nil)
	}
	_, err := sqlDB.Exec(`INSERT INTO env_stage(app,team,until) VALUES(?,?,?)
		ON CONFLICT(app) DO UPDATE SET team=excluded.team, until=excluded.until`, app, team, now.Add(envStageTTL).Unix())
	return err == nil
}

// heldByOther reports whether another team staged vars under this name, whether or not its reservation has lapsed: those vars must be cleared before anyone else deploys the name.
func (e *envStageTable) heldByOther(app, team string, _ time.Time) bool {
	cur, _, ok := e.owner(app)
	return ok && cur != team
}

func (e *envStageTable) release(app string) {
	_, _ = sqlDB.Exec(`DELETE FROM env_stage WHERE app=?`, app)
}
