import { LightningElement } from "lwc";
import value from "@salesforce/resourceUrl/GladeLwcOracleResource";

export default class Oracle extends LightningElement {
  label = "@salesforce/resourceUrl/";
  value = value;
}
