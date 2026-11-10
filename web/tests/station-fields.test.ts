import { describe, expect, test } from "bun:test"
import { fakeReviewBundle } from "../src/lib/fixtures.gen"
import {
	draftValues,
	parseDraftValues,
	suggestDeflationReason,
	suggestInternalNote,
	suggestTechnicalFeatures,
} from "../src/lib/station-fields"

describe("station field drafts", () => {
	test("round-trips the fields kept in a draft", () => {
		const parsed = parseDraftValues(
			draftValues({
				deflationReason: "Cut one hour.",
				internalNote: "Keep this internal.",
				note: "Nice board.",
				technicalFeatures: "Firmware and PCB.",
			}),
		)

		expect(parsed).toEqual({
			deflationReason: "Cut one hour.",
			internalNote: "Keep this internal.",
			note: "Nice board.",
			technicalFeatures: "Firmware and PCB.",
		})
	})

	test("ignores malformed draft values", () => {
		expect(parseDraftValues("not json")).toEqual({})
		expect(parseDraftValues("[]")).toEqual({})
	})
})

describe("feature and time suggestions", () => {
	test("suggests features from the description, BOM and files", () => {
		const bundle = fakeReviewBundle({
			bom: [
				{ link: "", name: "XIAO ESP32-C3", purpose: "", quantity: 1, unitCostCents: 850, vendor: "" },
				{ link: "", name: "MPU6050", purpose: "", quantity: 1, unitCostCents: 180, vendor: "" },
			],
			evidence: {
				files: [
					{ kind: "design", path: "hardware/starbie.kicad_pcb" },
					{ kind: "firmware", path: "firmware/starbie.ino" },
				],
			},
			submission: { description: "A tiny motion-controlled digital pet on a star-shaped board." },
		})

		const text = suggestTechnicalFeatures(bundle)
		expect(text).toContain("digital pet")
		expect(text).toContain("Custom PCB")
		expect(text).toContain("XIAO ESP32-C3")
		expect(text).toContain("Firmware")
	})

	test("builds the deflation reason from journal cuts", () => {
		const bundle = fakeReviewBundle({
			journals: [
				{
					approvedSeconds: 1800,
					at: "2026-01-01T00:00:00Z",
					clipCount: 0,
					coveredSeconds: 0,
					deflationReason: "tutorial",
					id: "a",
					media: [],
					mediaCount: 0,
					reviewerNote: "",
					seconds: 3600,
					text: "",
					title: "A",
				},
				{
					approvedSeconds: 1200,
					at: "2026-01-01T00:00:00Z",
					clipCount: 0,
					coveredSeconds: 0,
					deflationReason: "idle",
					id: "b",
					media: [],
					mediaCount: 0,
					reviewerNote: "",
					seconds: 1800,
					text: "",
					title: "B",
				},
			],
		})

		const text = suggestDeflationReason(bundle, 5400)
		expect(text).toContain("Cut 0.67h")
		expect(text).toContain("tutorial consumption")
		expect(text).toContain("idle gap")
	})
})

describe("internal note suggestions", () => {
	test("collects findings and failed station answers into the internal note", () => {
		const bundle = fakeReviewBundle({
			findings: [
				{
					createdAt: "2026-01-01T00:00:00Z",
					detail: "",
					evidence: "",
					externalId: "project",
					id: "f1",
					makerAction: "Add the Gerber ZIP.",
					openedBy: "",
					openedSubmissionId: "sub",
					openedVersion: 1,
					question: "",
					resolution: "",
					resolvedBy: "",
					resolvedVersion: 0,
					severity: "fixable",
					station: "triage",
					status: "open",
					summary: "Missing Gerber ZIP",
					updatedAt: "2026-01-01T00:00:00Z",
				},
			],
			stationAnswers: [
				{
					answer: "fail",
					artifactHash: "",
					evidence: "Looks generated.",
					id: "a1",
					questionKey: "readme_explains",
					reviewerId: "r",
					stationKey: "narrative",
					submissionId: "sub",
					updatedAt: "2026-01-01T00:00:00Z",
				},
			],
			stations: [
				{
					active: 1,
					description: "",
					key: "narrative",
					label: "README",
					questions: [
						{
							evidenceKind: "text",
							key: "readme_explains",
							prompt: "Does the README explain the build?",
							required: 1,
							sort: 0,
						},
					],
					sort: 1,
					stage: 1,
					themes: [],
				},
			],
		})

		const text = suggestInternalNote(bundle)
		expect(text).toContain("Missing Gerber ZIP")
		expect(text).toContain("Add the Gerber ZIP.")
		expect(text).toContain("README")
		expect(text).toContain("Looks generated.")
	})
})
