package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrReportNotFound — отчёт ещё не сгенерирован.
var ErrReportNotFound = errors.New("report not found")

// ReportStore — итоговые отчёты сессий (критерии §12, WP-11).
type ReportStore struct {
	db *sql.DB
	d  Dialect
}

// NewReportStore создаёт хранилище.
func NewReportStore(dbx *sql.DB, d Dialect) *ReportStore {
	return &ReportStore{db: dbx, d: d}
}

// ReportExtra — поля подробного отчёта (Итерация B): вердикт, разрыв до грейда,
// план на 2 недели, прогресс vs прошлые интервью (JSON).
type ReportExtra struct {
	Verdict            string
	GradeGap           string
	StudyPlan2Weeks    string // JSON: []string
	ProgressVsPrevious string // JSON: []ProgressEntry (см. interviewer)
}

// Save — upsert отчёта сессии.
func (s *ReportStore) Save(ctx context.Context, sessionID int64, overall float64,
	gradeRecommendation string, criteria, strengths, weaknesses, recommendations []byte,
	extra ReportExtra) error {
	_, err := s.db.ExecContext(ctx, s.d.q(`
		INSERT INTO reports (session_id, overall, grade_recommendation, criteria,
			strengths, weaknesses, recommendations, verdict, grade_gap,
			study_plan_2weeks, progress_vs_previous, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (session_id) DO UPDATE SET
			overall = excluded.overall,
			grade_recommendation = excluded.grade_recommendation,
			criteria = excluded.criteria,
			strengths = excluded.strengths,
			weaknesses = excluded.weaknesses,
			recommendations = excluded.recommendations,
			verdict = excluded.verdict,
			grade_gap = excluded.grade_gap,
			study_plan_2weeks = excluded.study_plan_2weeks,
			progress_vs_previous = excluded.progress_vs_previous,
			created_at = excluded.created_at`),
		sessionID, overall, gradeRecommendation, string(criteria),
		string(strengths), string(weaknesses), string(recommendations),
		extra.Verdict, extra.GradeGap, extra.StudyPlan2Weeks, extra.ProgressVsPrevious,
		nowRFC3339())
	if err != nil {
		return fmt.Errorf("save report: %w", err)
	}
	return nil
}

// Get — отчёт сессии (ErrReportNotFound, если не сгенерирован).
func (s *ReportStore) Get(ctx context.Context, sessionID int64) (overall float64,
	gradeRecommendation string, criteria, strengths, weaknesses, recommendations []byte,
	extra ReportExtra, err error) {
	err = s.db.QueryRowContext(ctx, s.d.q(`
		SELECT overall, grade_recommendation, criteria, strengths, weaknesses, recommendations,
			COALESCE(verdict, ''), COALESCE(grade_gap, ''),
			COALESCE(study_plan_2weeks, '[]'), COALESCE(progress_vs_previous, '[]')
		FROM reports WHERE session_id = ?`), sessionID).
		Scan(&overall, &gradeRecommendation, &criteria, &strengths, &weaknesses, &recommendations,
			&extra.Verdict, &extra.GradeGap, &extra.StudyPlan2Weeks, &extra.ProgressVsPrevious)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", nil, nil, nil, nil, ReportExtra{}, ErrReportNotFound
		}
		return 0, "", nil, nil, nil, nil, ReportExtra{}, fmt.Errorf("get report: %w", err)
	}
	return overall, gradeRecommendation, criteria, strengths, weaknesses, recommendations, extra, nil
}
