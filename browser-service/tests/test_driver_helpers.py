from dagents_browser.driver import element_from_node, normalize_max_elements


class _FakeNode:
    tag_name = "button"
    attributes = {"role": "button"}

    def get_all_children_text(self, max_depth=2):
        return "Login"


def test_normalize_max_elements():
    assert normalize_max_elements(0) == 150
    assert normalize_max_elements(200) == 200
    assert normalize_max_elements(999) == 500


def test_element_from_node_index_only():
    item = element_from_node(7, _FakeNode())
    assert item["index"] == 7
    assert item["tag"] == "button"
    assert item["text"] == "Login"
    assert "selector" not in item
